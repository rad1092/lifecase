# Lifecase 빠른 시작

**CI 단계가 마지막 줄을 찍고 끝나지 않는 문제를 재현하는 도구입니다.** 아직 실험실 단계의 프로젝트입니다.

빌드가 실행한 프로세스는 이미 끝났는데, 그 프로세스가 띄운 개발 서버, 파일 감시기, 컴파일러 데몬 같은 자손이 stdout/stderr를 계속 잡고 있을 수 있습니다. 그러면 이런 일이 생깁니다.

- Node `child_process`에서는 `exit`는 오지만 `close`는 오지 않습니다.
- `execFile` 콜백과 Python `subprocess.run(capture_output=True)`는 파이프가 닫힐 때까지 기다립니다.
- 작업은 타임아웃까지 멈춰 있거나, 출력이 잘린 채 성공으로 보고됩니다.

Lifecase의 네이티브 fixture는 이 상황을 그대로 만들어 줍니다. 부모는 바로 끝나고 자손 하나가 stop 신호를 받을 때까지 파이프를 잡고 있습니다.

빌드 러너, 테스트 실행기, 개발 도구, 작업 워커처럼 프로세스를 띄우고 "끝났다"를 판단하는 코드를 관리한다면 사용하세요. 가짜 프로세스로 충분하거나, 사용하는 라이브러리 자체 테스트로 충분하거나, 운영 환경용 프로세스 감독이 필요하다면 맞지 않습니다.

## Node·Python으로 먼저 실행하기

예제는 표준 라이브러리만 쓰므로 npm이나 pip 설치가 필요 없습니다. fixture는 [Releases](https://github.com/rad1092/lifecase/releases)에서 받거나(먼저 `.sha256` 체크섬 확인) CMake로 빌드합니다.

```sh
cmake -S . -B build && cmake --build build --config Release
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"
node examples/node/hang-demo.mjs
(cd examples/node && node --test)
python3 examples/python/test_subprocess.py
```

`hang-demo.mjs`를 실행하면 도구는 27ms 만에 끝나지만 Node 프로세스는 자손의 5초 감시 타이머가 끝날 때까지 종료되지 않습니다. 테스트는 다음을 고정합니다.

- `exit`와 `close`의 차이
- `execFile`이 자손을 기다린다는 점
- `timeout`이 발동하면 이미 끝난 프로세스를 **성공**으로 보고한다는 점
- Python `poll()`과 `communicate()`의 차이
- `run(timeout=...)`이 종료 코드 0인 프로세스에 `TimeoutExpired`를 낸다는 점
- 짧은 대기 후 프로세스 그룹을 끝내는 정책(POSIX 전용)

## .NET(CliWrap) 검사 실행하기

CliWrap을 감싸 자체 취소·출력 정책을 만든 팀을 위한 예제입니다. CliWrap 자체에도 충분한 수명주기 테스트가 있습니다. .NET 8 SDK와 운영체제·아키텍처에 맞는 `lifecase-fixture`가 필요합니다. [Releases](https://github.com/rad1092/lifecase/releases)의 fixture를 받거나 CMake 3.16 이상과 C++17 컴파일러로 직접 빌드하세요.

```sh
cmake -S . -B build
cmake --build build --config Release
```

macOS/Linux에서는 저장소 루트에서 다음을 실행합니다.

```sh
export LIFECASE_FIXTURE="$PWD/build/lifecase-fixture"
dotnet test dotnet/tests/Lifecase.CliWrap.Tests -c Release --logger trx
```

Windows의 Visual Studio 빌드는 PowerShell에서 실행합니다.

```powershell
$env:LIFECASE_FIXTURE = (Resolve-Path ./build/Release/lifecase-fixture.exe).Path
dotnet test dotnet/tests/Lifecase.CliWrap.Tests -c Release --logger trx
```

다운로드한 fixture를 쓴다면 그 실행 파일의 절대 경로를 설정하세요. CMake 생성기에 따라 빌드 결과 위치는 달라질 수 있습니다. 이 워크플로에는 Go가 필요하지 않습니다. C# 테스트 정책을 애플리케이션의 통합 테스트에 적용하는 방법과 실제 패키지 통합 범위는 [.NET 가이드](../dotnet/README.md)를 참고하세요.

배포 대상은 `linux-x64`, `macos-arm64`, `windows-x64`이며 파일 이름은 `lifecase-fixture-TARGET.zip`입니다. 함께 제공되는 SHA-256 체크섬으로 압축 파일을 검증하세요. 다른 조합은 직접 테스트하기 전까지 미검증입니다. macOS 서명·공증 배포는 제공한다고 주장하지 않으며, 다운로드 보안 정책상 실행할 수 없다면 로컬 소스 빌드를 사용하세요. 자세한 절차는 [release 가이드](release-process.md)에 있습니다.

## 결과를 해석하는 기준

부모 종료는 자손 종료나 파이프 EOF와 다릅니다. 자손이 파이프를 보유하면 부모가 종료한 뒤에도 읽기가 계속 기다릴 수 있습니다. await 취소와 프로세스 종료도 별개의 결과로 검사해야 합니다. 고수준 API가 이 단계들을 합쳐 제공하는 것은 설계 선택이므로, 자신의 완료 정책을 명시해서 테스트하세요.

Windows 네이티브 콘솔 신호는 v1에서 지원하지 않습니다. 파일을 통한 협조적 종료를 검사한 것을 CTRL+C 지원으로 해석하면 안 됩니다. `spawn-cancel`은 준비 완료 전 취소를 검사하고, `crash`는 덤프를 만들지 않는 코드 23 즉시 종료입니다. [제한 사항](limitations.md)에 각 시나리오 범위가 정리되어 있습니다.

## 개발자용 Go 검증기: 선택 사항

fixture 개발이나 다른 runner와의 공통 계약 검사가 필요하면 Go 1.24 이상의 검증기를 사용할 수 있습니다.

```sh
go run ./cmd/lifecase run --fixture ./build/lifecase-fixture --json reports/go.json --junit reports/go.xml
go install ./cmd/lifecase
lifecase run --fixture ./build/lifecase-fixture --scenario inherit --scenario flood
```

CLI 설치는 C++ fixture를 함께 설치하지 않습니다. Go 바이너리 설치 디렉터리가 `PATH`에 있어야 합니다. `--scenario`를 반복하면 일부 시나리오만 실행하며, `--json`을 생략하면 JSON을 stdout으로 출력합니다.

자신의 runner를 [JSON Lines 프로토콜 v1](protocol-v1.md)에 연결했다면 다음과 같이 실행합니다.

```sh
lifecase run --fixture ./build/lifecase-fixture --adapter ./my-runner-adapter --json reports/custom.json --junit reports/custom.xml
```

어댑터 인수는 `--adapter-arg`를 인수마다 반복하세요. 직접 만든 .NET DLL 어댑터는 `--adapter dotnet --adapter-arg /absolute/path/to/Your.Adapter.dll`로 실행할 수 있습니다. 포함된 CliWrap 테스트는 fixture를 직접 사용하므로 이 어댑터 프로토콜이 필요하지 않습니다.

JSON에는 개별 계약과 관측 시점이, JUnit에는 CI 테스트 결과가 기록됩니다. 종료 코드 `0`은 실패 계약 없음, `1`은 계약 실패, `2`는 설정 또는 실행 오류입니다. `unsupported`는 검증하지 않은 기능이며 JUnit의 skipped로 표시됩니다. 시나리오 이름은 일정하지만 PID와 소요 시간은 변하므로 전체 JSON 문자열 대신 개별 판정을 비교하세요.

## CI와 안전

배포 대상인 Linux/macOS/Windows에서 각각 실행하고 결과를 보관하세요. 저장소의 [CI workflow](../.github/workflows)와 사용할 커밋의 실행 결과를 확인해야 합니다. 한 운영체제의 성공이 다른 환경을 보장하지 않습니다.

fixture는 합성 데이터, 새 임시 디렉터리, 실행별 토큰, 최대 한 자손과 유한 watchdog을 사용합니다. 자신이 생성한 프로세스만 종료하며 사용자의 다른 프로그램을 PID나 이름으로 찾아 종료하지 않습니다. watchdog은 최후의 시간 제한 장치로, 정상 종료 성공을 대신하지 않습니다. 어댑터는 사용자 권한으로 실행되므로 신뢰할 수 있는 것만 사용하세요.
