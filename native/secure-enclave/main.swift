import CryptoKit
import Foundation
import LocalAuthentication
import Security

func publicKeyHex(_ key: P256.Signing.PublicKey) -> String {
    let point = key.x963Representation
    let compressed = Data([2 | (point.last! & 1)]) + point[1..<33]
    return compressed.map { String(format: "%02x", $0) }.joined()
}

// A single bounded stdin request, no arguments, logging, network or file storage.
// CryptoKit's dataRepresentation is an opaque device-bound wrapped reference,
// not an exported P-256 private scalar. It is persisted only in the OS vault.
func handle(_ request: [String: String]) throws -> [String: String] {
    if request["action"] == "availability" {
        guard Set(request.keys) == ["action"] else { return ["error": "invalid_request"] }
        return ["availability": SecureEnclave.isAvailable ? "available" : "unsupported"]
    }
    guard SecureEnclave.isAvailable else { return ["error": "hardware_unavailable"] }
    let context = LAContext()
    context.interactionNotAllowed = true
    switch request["action"] {
    case "create":
        guard Set(request.keys) == ["action"] else { return ["error": "invalid_request"] }
        // No userPresence/biometry requirement: existing server authorization,
        // expiry, policy and limits still govern every financial operation.
        guard let access = SecAccessControlCreateWithFlags(
            nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .privateKeyUsage, nil
        ) else { return ["error": "access_control_unavailable"] }
        let key = try SecureEnclave.P256.Signing.PrivateKey(
            accessControl: access, authenticationContext: context
        )
        return [
            "reference": key.dataRepresentation.base64EncodedString(),
            "publicKey": publicKeyHex(key.publicKey),
        ]
    case "sign":
        guard Set(request.keys) == ["action", "reference", "publicKey", "payload"],
              let reference = Data(base64Encoded: request["reference"]!),
              !reference.isEmpty, reference.count <= 1024,
              let payload = Data(base64Encoded: request["payload"]!)
        else { return ["error": "invalid_request"] }
        let key = try SecureEnclave.P256.Signing.PrivateKey(
            dataRepresentation: reference, authenticationContext: context
        )
        let publicKey = publicKeyHex(key.publicKey)
        guard publicKey == request["publicKey"] else { return ["error": "key_mismatch"] }
        // Match Go's ECDSA over SHA-256 of the exact protocol payload.
        let signature = try key.signature(for: payload)
        return ["signature": signature.derRepresentation.base64EncodedString()]
    default:
        return ["error": "invalid_request"]
    }
}

do {
    let input = FileHandle.standardInput.readData(ofLength: 1024 * 1024 + 1)
    let response: [String: String]
    if input.count <= 1024 * 1024,
       let request = try JSONSerialization.jsonObject(with: input) as? [String: String] {
        response = try handle(request)
    } else {
        response = ["error": "invalid_request"]
    }
    FileHandle.standardOutput.write(try JSONSerialization.data(withJSONObject: response))
} catch {
    // Native diagnostics may include key references. Return only a fixed code.
    FileHandle.standardOutput.write(Data("{\"error\":\"key_unavailable\"}".utf8))
}
