// svg2png renders an SVG to a square PNG that keeps its transparency, using
// AppKit (macOS 13+), so make icon needs nothing installed. qlmanage, used
// before, paints onto white: the Dock then showed a white square around the
// rounded tile.
//
// usage: swift scripts/svg2png.swift <in.svg> <out.png> <size>
import AppKit
let args = CommandLine.arguments
guard args.count == 4, let size = Int(args[3]), let img = NSImage(contentsOfFile: args[1]) else {
  FileHandle.standardError.write("usage: svg2png <in.svg> <out.png> <size>\n".data(using: .utf8)!); exit(1)
}
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8,
  samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
img.draw(in: NSRect(x: 0, y: 0, width: size, height: size))
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[2]))
