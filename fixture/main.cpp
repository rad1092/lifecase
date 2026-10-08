// Controlled synthetic subprocess fixture. Protocol: docs/protocol-v1.md.
// The fixture never finds or terminates another process by PID.
#include <algorithm>
#include <array>
#include <cerrno>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#else
#include <csignal>
#include <fcntl.h>
#include <spawn.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#ifdef __APPLE__
#include <mach-o/dyld.h>
#endif
extern char** environ;
#endif

namespace {
namespace fs = std::filesystem;
using Clock = std::chrono::steady_clock;
using namespace std::chrono_literals;
constexpr std::size_t kFloodBytes = 262144;

struct Options {
  std::string scenario;
  fs::path directory;
  std::string token;
  int lease_ms = 5000;
  bool child = false;
};

class Watchdog {
 public:
  explicit Watchdog(int lease_ms) : thread_([this, lease_ms] {
    std::unique_lock<std::mutex> lock(mutex_);
    if (!cv_.wait_for(lock, std::chrono::milliseconds(lease_ms), [this] { return finished_; })) {
      // No buffered I/O, atexit handlers, or joins: also bounds blocked writes.
      std::_Exit(124);
    }
  }) {}
  Watchdog(const Watchdog&) = delete;
  Watchdog& operator=(const Watchdog&) = delete;
  ~Watchdog() {
    {
      std::lock_guard<std::mutex> lock(mutex_);
      finished_ = true;
    }
    cv_.notify_one();
    thread_.join();
  }
 private:
  std::mutex mutex_;
  std::condition_variable cv_;
  bool finished_ = false;
  std::thread thread_;
};

std::uint64_t process_id() {
#ifdef _WIN32
  return GetCurrentProcessId();
#else
  return static_cast<std::uint64_t>(getpid());
#endif
}

#ifdef _WIN32
std::string utf8(const std::wstring& value) {
  if (value.empty()) return {};
  const int size = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value.data(),
      static_cast<int>(value.size()), nullptr, 0, nullptr, nullptr);
  if (size == 0) throw std::runtime_error("invalid Unicode argument");
  std::string result(static_cast<std::size_t>(size), '\0');
  if (WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value.data(),
      static_cast<int>(value.size()), result.data(), size, nullptr, nullptr) != size) {
    throw std::runtime_error("Unicode argument conversion failed");
  }
  return result;
}

// The MSVC runtime interprets runs of backslashes before quotes specially.
// https://learn.microsoft.com/en-us/cpp/c-language/parsing-c-command-line-arguments
std::wstring quote_argument(const std::wstring& argument) {
  std::wstring result = L"\"";
  std::size_t slashes = 0;
  for (const wchar_t c : argument) {
    if (c == L'\\') {
      ++slashes;
    } else {
      result.append(slashes * (c == L'"' ? 2 : 1), L'\\');
      if (c == L'"') result += L'\\';
      result += c;
      slashes = 0;
    }
  }
  result.append(slashes * 2, L'\\');
  result += L'"';
  return result;
}

class Handle {
 public:
  explicit Handle(HANDLE value = INVALID_HANDLE_VALUE) : value_(value) {}
  Handle(const Handle&) = delete;
  Handle& operator=(const Handle&) = delete;
  ~Handle() { if (valid()) CloseHandle(value_); }
  bool valid() const { return value_ != nullptr && value_ != INVALID_HANDLE_VALUE; }
  HANDLE get() const { return value_; }
 private:
  HANDLE value_;
};
#else
volatile std::sig_atomic_t terminated = 0;
void term_handler(int) { terminated = 1; }
#endif

fs::path executable_path() {
#ifdef _WIN32
  std::wstring path(32768, L'\0');
  const DWORD size = GetModuleFileNameW(nullptr, path.data(), static_cast<DWORD>(path.size()));
  if (size == 0 || size >= path.size()) throw std::runtime_error("cannot locate fixture executable");
  path.resize(size);
  return fs::path(path);
#elif defined(__APPLE__)
  std::uint32_t size = 0;
  _NSGetExecutablePath(nullptr, &size);
  std::vector<char> path(size);
  if (_NSGetExecutablePath(path.data(), &size) != 0) throw std::runtime_error("cannot locate fixture executable");
  return fs::canonical(path.data());
#else
  std::array<char, 32768> path{};
  const ssize_t size = readlink("/proc/self/exe", path.data(), path.size());
  if (size <= 0 || static_cast<std::size_t>(size) == path.size()) throw std::runtime_error("cannot locate fixture executable");
  return fs::path(std::string(path.data(), static_cast<std::size_t>(size)));
#endif
}

Options parse(const std::vector<std::string>& args) {
  Options options;
  std::vector<std::string> seen;
  for (std::size_t i = 1; i < args.size(); ++i) {
    const auto& key = args[i];
    if (std::find(seen.begin(), seen.end(), key) != seen.end()) throw std::runtime_error("duplicate option");
    seen.push_back(key);
    if (key == "--child") {
      options.child = true;
      continue;
    }
    if (++i >= args.size()) throw std::runtime_error("missing option value");
    const auto& value = args[i];
    if (key == "--scenario") options.scenario = value;
    else if (key == "--dir") options.directory = fs::u8path(value);
    else if (key == "--token") options.token = value;
    else if (key == "--lease-ms") {
      if (value.empty() || value.size() > 5 || !std::all_of(value.begin(), value.end(), [](char c) { return c >= '0' && c <= '9'; })) {
        throw std::runtime_error("lease must be an integer from 1000 to 10000");
      }
      options.lease_ms = std::stoi(value);
    } else throw std::runtime_error("unknown option");
  }
  const std::array<const char*, 10> scenarios{{"exit", "flood", "stdin-blocked", "cooperative", "uncooperative",
      "crash", "inherit", "startup-timeout", "spawn-cancel", "signal"}};
  if (std::find(scenarios.begin(), scenarios.end(), options.scenario) == scenarios.end()) throw std::runtime_error("unknown scenario");
  if (options.child && options.scenario != "inherit") throw std::runtime_error("child mode requires inherit scenario");
  if (!options.directory.is_absolute()) throw std::runtime_error("--dir must be an absolute private directory");
  if (options.token.size() != 32 || !std::all_of(options.token.begin(), options.token.end(), [](char c) {
        return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
      })) throw std::runtime_error("--token must be 32 lowercase hexadecimal characters");
  if (options.lease_ms < 1000 || options.lease_ms > 10000) throw std::runtime_error("lease must be from 1000 to 10000 ms");
  return options;
}

void validate_directory(const fs::path& path) {
  const auto status = fs::symlink_status(path);
  if (!fs::is_directory(status)) throw std::runtime_error("--dir must be an existing non-symlink directory");
#ifndef _WIN32
  struct stat info{};
  if (stat(path.c_str(), &info) != 0 || info.st_uid != geteuid() || (info.st_mode & 0077) != 0) {
    throw std::runtime_error("--dir must be owned by this user with mode 0700");
  }
#endif
}

bool matching_file(const fs::path& path, const std::string& expected) {
  std::error_code error;
  const auto status = fs::symlink_status(path, error);
  if (error || !fs::is_regular_file(status)) return false;
  const auto size = fs::file_size(path, error);
  if (error || size > 1024) return false;
  std::ifstream input(path, std::ios::binary);
  std::array<char, 1025> bytes{};
  input.read(bytes.data(), static_cast<std::streamsize>(bytes.size()));
  const std::string actual(bytes.data(), static_cast<std::size_t>(input.gcount()));
  return actual == expected || actual == expected + "\n";
}

bool stopped(const Options& options) {
  return options.scenario != "uncooperative" && matching_file(options.directory / "stop", options.token);
}

std::string event_json(const Options& options, std::uint64_t pid, const std::string& role, const std::string& event) {
  return "{\"v\":1,\"token\":\"" + options.token + "\",\"pid\":" + std::to_string(pid) +
      ",\"role\":\"" + role + "\",\"event\":\"" + event + "\"}";
}

void write_event(const Options& options, const std::string& role, const std::string& event) {
  const auto target = options.directory / (role + "-" + event + ".json");
  const auto temporary = options.directory / ("." + role + "-" + event + "." + std::to_string(process_id()) + ".tmp");
  const std::string content = event_json(options, process_id(), role, event) + "\n";
#ifdef _WIN32
  {
    Handle file(CreateFileW(temporary.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, nullptr));
    if (!file.valid()) throw std::runtime_error("cannot create event file");
    DWORD written = 0;
    if (!WriteFile(file.get(), content.data(), static_cast<DWORD>(content.size()), &written, nullptr) || written != content.size()) {
      throw std::runtime_error("cannot write event file");
    }
  }
  // No replacement: fresh token-owned directories are required.
  if (!MoveFileExW(temporary.c_str(), target.c_str(), 0)) throw std::runtime_error("cannot publish event file");
#else
  const int fd = open(temporary.c_str(), O_CREAT | O_EXCL | O_WRONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
  if (fd < 0) throw std::runtime_error("cannot create event file");
  std::size_t total = 0;
  while (total < content.size()) {
    const ssize_t n = write(fd, content.data() + total, content.size() - total);
    if (n < 0 && errno == EINTR) continue;
    if (n <= 0) { close(fd); throw std::runtime_error("cannot write event file"); }
    total += static_cast<std::size_t>(n);
  }
  if (close(fd) != 0) throw std::runtime_error("cannot close event file");
  // The directory is owned/private. Reject stale targets before atomic rename.
  if (fs::exists(fs::symlink_status(target)) || rename(temporary.c_str(), target.c_str()) != 0) {
    throw std::runtime_error("cannot publish event file");
  }
#endif
}

bool write_bytes(bool stderr_stream, char value, std::size_t count) {
  std::array<char, 4096> data{};
  data.fill(value);
  while (count > 0) {
    const auto size = std::min(count, data.size());
#ifdef _WIN32
    DWORD written = 0;
    if (!WriteFile(GetStdHandle(stderr_stream ? STD_ERROR_HANDLE : STD_OUTPUT_HANDLE), data.data(),
          static_cast<DWORD>(size), &written, nullptr) || written == 0) return false;
    count -= written;
#else
    const ssize_t written = write(stderr_stream ? STDERR_FILENO : STDOUT_FILENO, data.data(), size);
    if (written < 0 && errno == EINTR) continue;
    if (written <= 0) return false;
    count -= static_cast<std::size_t>(written);
#endif
  }
  return true;
}

std::uint64_t spawn_child(const Options& options) {
  const auto executable = executable_path();
#ifdef _WIN32
  const std::vector<std::wstring> args{executable.native(), L"--scenario", L"inherit", L"--dir", options.directory.native(),
      L"--token", std::wstring(options.token.begin(), options.token.end()), L"--lease-ms", std::to_wstring(options.lease_ms), L"--child"};
  std::wstring command;
  for (const auto& argument : args) { if (!command.empty()) command += L' '; command += quote_argument(argument); }
  if (command.size() >= 32767) throw std::runtime_error("child command exceeds Windows command line limit");

  // Give only three duplicated standard handles to the child. Do not inherit
  // unrelated handles from a test subject, and never share a parent control handle.
  // https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw
  std::array<HANDLE, 3> inherited{};
  const std::array<DWORD, 3> standard{{STD_INPUT_HANDLE, STD_OUTPUT_HANDLE, STD_ERROR_HANDLE}};
  for (std::size_t i = 0; i < inherited.size(); ++i) {
    if (!DuplicateHandle(GetCurrentProcess(), GetStdHandle(standard[i]), GetCurrentProcess(), &inherited[i], 0, TRUE, DUPLICATE_SAME_ACCESS)) {
      for (std::size_t j = 0; j < i; ++j) CloseHandle(inherited[j]);
      throw std::runtime_error("cannot duplicate standard handles");
    }
  }
  const Handle input(inherited[0]), output(inherited[1]), error(inherited[2]);
  SIZE_T bytes = 0;
  InitializeProcThreadAttributeList(nullptr, 1, 0, &bytes);
  std::vector<unsigned char> storage(bytes);
  auto* attributes = reinterpret_cast<LPPROC_THREAD_ATTRIBUTE_LIST>(storage.data());
  if (!InitializeProcThreadAttributeList(attributes, 1, 0, &bytes)) throw std::runtime_error("cannot initialize handle list");
  struct AttributeGuard {
    LPPROC_THREAD_ATTRIBUTE_LIST value;
    ~AttributeGuard() { DeleteProcThreadAttributeList(value); }
  } guard{attributes};
  if (!UpdateProcThreadAttribute(attributes, 0, PROC_THREAD_ATTRIBUTE_HANDLE_LIST, inherited.data(), sizeof(inherited), nullptr, nullptr)) {
    throw std::runtime_error("cannot restrict inherited handles");
  }
  STARTUPINFOEXW startup{};
  startup.StartupInfo.cb = static_cast<DWORD>(sizeof(startup));
  startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
  startup.StartupInfo.hStdInput = input.get();
  startup.StartupInfo.hStdOutput = output.get();
  startup.StartupInfo.hStdError = error.get();
  startup.lpAttributeList = attributes;
  PROCESS_INFORMATION child{};
  if (!CreateProcessW(executable.c_str(), command.data(), nullptr, nullptr, TRUE, EXTENDED_STARTUPINFO_PRESENT,
      nullptr, nullptr, &startup.StartupInfo, &child)) throw std::runtime_error("cannot spawn owned child");
  const Handle process(child.hProcess), thread(child.hThread);
  return child.dwProcessId;
#else
  std::vector<std::string> args{executable.string(), "--scenario", "inherit", "--dir", options.directory.string(),
      "--token", options.token, "--lease-ms", std::to_string(options.lease_ms), "--child"};
  std::vector<char*> argv;
  for (auto& argument : args) argv.push_back(argument.data());
  argv.push_back(nullptr);
  pid_t child = -1;
  // posix_spawn is safe with our active watchdog thread; no post-fork C++ runs.
  // Standard descriptors intentionally stay open in the child.
  // https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/posix_spawn.2.html
  const int error = posix_spawn(&child, executable.c_str(), nullptr, nullptr, argv.data(), environ);
  if (error != 0) throw std::runtime_error("cannot spawn owned child: " + std::string(std::strerror(error)));
  return static_cast<std::uint64_t>(child);
#endif
}

int run(const Options& options) {
  Watchdog watchdog(options.lease_ms);
  validate_directory(options.directory);
#ifndef _WIN32
  struct sigaction ignored{};
  ignored.sa_handler = SIG_IGN;
  sigemptyset(&ignored.sa_mask);
  if (sigaction(SIGPIPE, &ignored, nullptr) != 0) throw std::runtime_error("cannot install SIGPIPE policy");
  if (options.scenario == "signal") {
    struct sigaction handler{};
    handler.sa_handler = term_handler;
    sigemptyset(&handler.sa_mask);
    if (sigaction(SIGTERM, &handler, nullptr) != 0) throw std::runtime_error("cannot install signal handler");
  }
#else
  if (options.scenario == "signal") {
    std::cerr << "lifecase-fixture: Windows console signal scenario unsupported in protocol 1\n";
    return 78;
  }
#endif
  if (options.child) {
    write_event(options, "child", "ready");
    while (!stopped(options)) std::this_thread::sleep_for(5ms);
    write_event(options, "child", "exit");
    return 0;
  }
  if (options.scenario == "startup-timeout" || options.scenario == "spawn-cancel") {
    const auto ready_at = Clock::now() + 2000ms;
    while (Clock::now() < ready_at) {
      if (stopped(options)) return 0;
      std::this_thread::sleep_for(5ms);
    }
  }
  if (options.scenario == "inherit") {
    if (stopped(options)) return 0;
    const auto child = spawn_child(options);
    const auto ready = event_json(options, child, "child", "ready");
    while (!matching_file(options.directory / "child-ready.json", ready)) {
      if (stopped(options)) return 0;
      std::this_thread::sleep_for(5ms);
    }
  }
  write_event(options, "parent", "ready");
  while (!matching_file(options.directory / "go", options.token)) {
    if (stopped(options)) return 0;
#ifndef _WIN32
    if (options.scenario == "signal" && terminated != 0) return 0;
#endif
    std::this_thread::sleep_for(5ms);
  }
  if (stopped(options)) return 0;
  if (options.scenario == "exit") {
    return write_bytes(false, 'o', 1) && write_bytes(false, 'k', 1) && write_bytes(false, '\n', 1) ? 0 : 74;
  }
  if (options.scenario == "flood") {
    bool err_ok = false;
    std::thread stderr_writer([&err_ok] { err_ok = write_bytes(true, 'E', kFloodBytes); });
    const bool out_ok = write_bytes(false, 'O', kFloodBytes);
    stderr_writer.join();
    return out_ok && err_ok ? 0 : 74;
  }
  if (options.scenario == "crash") std::_Exit(23);
  if (options.scenario == "inherit") return 0;
  while (!stopped(options)) {
#ifndef _WIN32
    if (options.scenario == "signal" && terminated != 0) return 0;
#endif
    std::this_thread::sleep_for(5ms);
  }
  return 0;
}

int main_impl(const std::vector<std::string>& args) {
  try {
    if (args.size() == 2 && args[1] == "--version") {
      std::cout << "lifecase-fixture 0.1.0 protocol 1\n";
      return 0;
    }
    if (args.size() == 2 && args[1] == "--help") {
      std::cout << "Usage: lifecase-fixture --scenario NAME --dir ABSOLUTE_PRIVATE_DIR --token HEX32 [--lease-ms 5000]\n"
          "Scenarios: exit flood stdin-blocked cooperative uncooperative crash inherit startup-timeout spawn-cancel signal\n"
          "Every process exits within its 1000..10000 ms lease. See docs/protocol-v1.md.\n";
      return 0;
    }
    return run(parse(args));
  } catch (const std::exception& error) {
    std::cerr << "lifecase-fixture: " << std::string(error.what()).substr(0, 400) << '\n';
    return 64;
  }
}
}  // namespace

#ifdef _WIN32
int wmain(int argc, wchar_t* argv[]) {
  try {
    std::vector<std::string> args;
    for (int i = 0; i < argc; ++i) args.push_back(utf8(argv[i]));
    return main_impl(args);
  } catch (const std::exception&) {
    std::cerr << "lifecase-fixture: invalid Unicode argument\n";
    return 64;
  }
}
#else
int main(int argc, char* argv[]) {
  return main_impl(std::vector<std::string>(argv, argv + argc));
}
#endif
