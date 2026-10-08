# Lifecase 빠른 시작

Lifecase는 subprocess 완료·취소 정책을 검사하기 위한 작은 네이티브 fixture 모음입니다. 첫 통합 예제는 .NET과 실제 CliWrap 패키지를 사용합니다. 부모 종료, stdout/stderr의 실제 EOF, 자손 정리, 정상·강제 종료를 구분해 애플리케이션의 정책을 검사할 때 사용하세요.

CliWrap 자체에도 충분한 수명주기 테스트가 있습니다. 기존 테스트가 요구를 충족한다면 이 도구를 추가할 필요가 없습니다. Lifecase의 도입 이유와 비용은 [조사 문서](research.md)에 정리했습니다.

## .NET 검사부터 실행하기

.NET 8 SDK와 운영체제·아키텍처에 맞는 `lifecase-fixture`가 필요합니다. [Releases](https://github.com/rad1092/lifecase/releases)의 fixture를 받거나 CMake 3.16 이상과 C++17 컴파일러로 직접 빌드하세요.

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
