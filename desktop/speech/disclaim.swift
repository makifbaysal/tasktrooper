import Darwin
import Foundation

@_silgen_name("responsibility_spawnattrs_setdisclaim")
func responsibility_spawnattrs_setdisclaim(_ attrs: UnsafeMutablePointer<posix_spawnattr_t?>, _ disclaim: Int32) -> Int32

private var child: pid_t = 0

/// Re-execs this binary as its own TCC-responsible process and returns only in
/// that child. Without it macOS attributes speech recognition to whatever
/// started us and kills us (SIGABRT) when that app's Info.plist has no
/// NSSpeechRecognitionUsageDescription: a dev Electron, a terminal. Disclaimed,
/// the usage string linked into this binary (Info.plist) is the one read.
/// The parent stays as a thin wrapper so the pid Electron holds is the one that
/// exits; a SIGTERM to it is passed on.
func becomeResponsibleProcess() {
  if ProcessInfo.processInfo.environment["TT_SPEECH_DISCLAIMED"] == "1" { return }
  setenv("TT_SPEECH_DISCLAIMED", "1", 1)
  var attr: posix_spawnattr_t?
  posix_spawnattr_init(&attr)
  _ = responsibility_spawnattrs_setdisclaim(&attr, 1)
  let argv: [UnsafeMutablePointer<CChar>?] = CommandLine.arguments.map { strdup($0) } + [nil]
  let path = Bundle.main.executablePath ?? CommandLine.arguments[0]
  let rc = posix_spawn(&child, path, nil, &attr, argv, environ)
  if rc != 0 {
    print("{\"type\":\"error\",\"code\":\"spawn\",\"message\":\"could not start speech recognition (\(rc))\"}")
    exit(1)
  }
  for sig in [SIGTERM, SIGINT, SIGHUP] {
    signal(sig) { received in
      kill(child, received)
    }
  }
  var status: Int32 = 0
  while waitpid(child, &status, 0) == -1 && errno == EINTR {}
  let signaled = (status & 0x7f) != 0
  exit(signaled ? 128 + (status & 0x7f) : (status >> 8) & 0xff)
}
