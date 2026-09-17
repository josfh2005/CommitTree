// swift-tools-version:6.2
import PackageDescription

let package = Package(
    name: "git-ui-apple",
    platforms: [.macOS(.v26)],
    targets: [
        .executableTarget(name: "git-ui-apple", path: "Sources/git-ui-apple")
    ]
)
