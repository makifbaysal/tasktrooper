import AVFoundation
import Foundation
import Speech

// tt-speech <locale>: one utterance per process. stdin is 16 kHz mono s16le
// PCM; closing it ends the utterance. stdout is one JSON object per line:
// {"type":"ready"}, {"type":"partial","text":…}, {"type":"final","text":…},
// {"type":"error","code":…,"message":…}. Exit 0 after "final".

becomeResponsibleProcess()
setvbuf(stdout, nil, _IOLBF, 0)

func emit(_ obj: [String: Any]) {
  if let data = try? JSONSerialization.data(withJSONObject: obj), let line = String(data: data, encoding: .utf8) {
    print(line)
  }
}

func fail(_ code: String, _ message: String) -> Never {
  emit(["type": "error", "code": code, "message": message])
  exit(1)
}

let supported: Set<String> = ["tr-TR", "en-US"]
let localeId = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ""
guard supported.contains(localeId) else { fail("locale", "unsupported locale \(localeId)") }

let pcm = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16000, channels: 1, interleaved: true)!
var running: SFSpeechRecognitionTask?

func start() {
  guard let recognizer = SFSpeechRecognizer(locale: Locale(identifier: localeId)), recognizer.isAvailable else {
    fail("unavailable", "speech recognition is not available for \(localeId)")
  }
  let request = SFSpeechAudioBufferRecognitionRequest()
  request.shouldReportPartialResults = true
  // On-device whenever this Mac has the model: nothing the user says leaves it.
  request.requiresOnDeviceRecognition = recognizer.supportsOnDeviceRecognition
  request.addsPunctuation = true
  var last = ""
  running = recognizer.recognitionTask(with: request) { result, error in
    if let result {
      last = result.bestTranscription.formattedString
      if result.isFinal {
        emit(["type": "final", "text": last])
        exit(0)
      }
      emit(["type": "partial", "text": last])
    }
    if let error {
      // Ending the audio with nothing said finishes with "no speech detected";
      // that is an empty utterance, not a failure.
      let ns = error as NSError
      if ns.domain == "kAFAssistantErrorDomain" && (ns.code == 1110 || ns.code == 216) {
        emit(["type": "final", "text": last])
        exit(0)
      }
      fail("recognition", error.localizedDescription)
    }
  }
  emit(["type": "ready", "onDevice": request.requiresOnDeviceRecognition])
  DispatchQueue.global(qos: .userInitiated).async {
    while true {
      let data = FileHandle.standardInput.readData(ofLength: 3200)
      if data.isEmpty { break }
      let frames = AVAudioFrameCount(data.count / 2)
      guard frames > 0, let buffer = AVAudioPCMBuffer(pcmFormat: pcm, frameCapacity: frames) else { continue }
      buffer.frameLength = frames
      data.withUnsafeBytes { raw in
        memcpy(buffer.int16ChannelData![0], raw.baseAddress!, Int(frames) * 2)
      }
      request.append(buffer)
    }
    request.endAudio()
  }
}

// The authorization callback arrives on an arbitrary queue, and the first one
// shows the system prompt; the main queue must stay free for both.
SFSpeechRecognizer.requestAuthorization { status in
  switch status {
  case .authorized:
    DispatchQueue.main.async { start() }
  case .denied, .restricted:
    fail("denied", "speech recognition is turned off for TaskTrooper in System Settings")
  default:
    fail("denied", "speech recognition was not allowed")
  }
}
dispatchMain()
