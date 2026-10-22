import AppKit

// Draw terminal text at scalar cell positions, keeping the preview's document
// wider than its viewport. The outer dialog never grows with a long line.
final class ScalarPreview: NSView {
  let lines: [String]
  let font = NSFont.monospacedSystemFont(ofSize: Look.small, weight: .regular)
  var cell: CGFloat { ("M" as NSString).size(withAttributes: [.font: font]).width }
  var lineHeight: CGFloat { font.ascender - font.descender + font.leading }
  override var isFlipped: Bool { true }

  init(_ text: String) {
    lines = text.components(separatedBy: "\n")
    super.init(frame: .zero)
    let columns = lines.map { $0.unicodeScalars.reduce(0) { $0 + scalarWidth($1) } }.max() ?? 0
    frame.size = NSSize(width: CGFloat(columns) * cell, height: CGFloat(lines.count) * lineHeight)
  }
  required init?(coder: NSCoder) { fatalError() }
  override func draw(_ dirtyRect: NSRect) {
    NSColor.textBackgroundColor.setFill()
    dirtyRect.fill()
    for (row, line) in lines.enumerated() {
      var x: CGFloat = 0
      var text = ""
      var width = 0
      func drawText() {
        (text as NSString).draw(at: NSPoint(x: x, y: CGFloat(row) * lineHeight), withAttributes: [.font: font, .foregroundColor: NSColor.labelColor])
        x += CGFloat(width) * cell
      }
      for scalar in line.unicodeScalars {
        let cells = scalarWidth(scalar)
        if cells > 0 && !text.isEmpty { drawText(); text = ""; width = 0 }
        text += String(scalar)
        width += cells
      }
      if !text.isEmpty { drawText() }
    }
  }
}

func previewScroll(_ text: String) -> NSScrollView {
  let document = ScalarPreview(text)
  let scroll = NSScrollView()
  scroll.translatesAutoresizingMaskIntoConstraints = false
  scroll.hasHorizontalScroller = true
  scroll.hasVerticalScroller = false
  scroll.autohidesScrollers = true
  scroll.documentView = document
  scroll.heightAnchor.constraint(equalToConstant: document.frame.height + NSScroller.scrollerWidth(for: .regular, scrollerStyle: .legacy)).isActive = true
  return scroll
}
