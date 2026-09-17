import Foundation
import FoundationModels

struct Input: Decodable {
    let instructions: String
    let prompt: String
}

func emit(_ object: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: object) else { return }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data("\n".utf8))
}

func status() {
    switch SystemLanguageModel.default.availability {
    case .available:
        emit(["available": true])
    case .unavailable(let reason):
        let name: String
        switch reason {
        case .deviceNotEligible: name = "deviceNotEligible"
        case .appleIntelligenceNotEnabled: name = "appleIntelligenceNotEnabled"
        case .modelNotReady: name = "modelNotReady"
        @unknown default: name = "unknown"
        }
        emit(["available": false, "reason": name])
    }
}

func respond() async -> Int32 {
    let data = FileHandle.standardInput.readDataToEndOfFile()
    guard let input = try? JSONDecoder().decode(Input.self, from: data) else {
        emit(["error": "invalid input"])
        return 1
    }
    let session = LanguageModelSession(instructions: input.instructions)
    var sent = ""
    do {
        for try await snapshot in session.streamResponse(to: input.prompt) {
            let text = snapshot.content
            if text.hasPrefix(sent) {
                let delta = String(text.dropFirst(sent.count))
                if !delta.isEmpty { emit(["delta": delta]) }
            } else {
                emit(["delta": text])
            }
            sent = text
        }
        emit(["done": true])
        return 0
    } catch let error as LanguageModelSession.GenerationError {
        switch error {
        case .exceededContextWindowSize:
            emit(["error": "The commit is too large for Apple Intelligence's context window."])
        case .guardrailViolation:
            emit(["error": "Apple Intelligence declined this request (safety guardrail)."])
        default:
            emit(["error": "Apple Intelligence failed: \(error)"])
        }
        return 1
    } catch {
        emit(["error": "Apple Intelligence failed: \(error)"])
        return 1
    }
}

let command = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : ""
switch command {
case "status":
    status()
case "respond":
    exit(await respond())
default:
    FileHandle.standardError.write(Data("usage: git-ui-apple status|respond\n".utf8))
    exit(2)
}
