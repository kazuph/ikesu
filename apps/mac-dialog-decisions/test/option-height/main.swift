import AppKit
import Foundation

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root = CommandLine.arguments[1]
let recordOnly = CommandLine.arguments.contains("--record-before")
let capture = ProcessInfo.processInfo.environment["IKESU_OPTION_CAPTURE"] == "1"
func descendants(_ view: NSView) -> [NSView] { view.subviews.flatMap { [$0] + descendants($0) } }
var metrics: [[String: Any]] = []
var failures: [String] = []
// The minimum supported width exercises real text wrapping without adding rows.
for width in [Look.width, CGFloat(720)] {
 for count in [2, 3, 4] {
  for theme in [NSAppearance.Name.aqua, .darkAqua] {
   app.appearance = NSAppearance(named: theme)
   let descriptions = ["変更する範囲と実行前に必要な手順をすべて確認します。\n実行する内容を確認します。\n結果を保存します。\n確認してから回答します。", "", "今回だけ許可", "説明が長くても全文を表示し、カードの外にはみ出さずに読めることを確認します。"]
   let options = (0..<count).map { ["id": "choice-\($0)", "label": "選択肢 \($0 + 1)", "description": descriptions[$0]] }
   let input: [String: Any] = ["window_title": "選択肢の高さ・隔離試験", "title": "説明の行数が違う選択肢", "body": "横に並ぶカードの高さと文字の表示を確認します。", "location": "試験用Mac｜ikesu｜隔離試験｜自前パネル", "options": options, "allow_text": true, "multi_select": false]
   let spec = try JSONDecoder().decode(DialogSpec.self, from: JSONSerialization.data(withJSONObject: input))
   let controller = DialogController(spec: spec, specPath: root + "/spec.json")
   controller.build()
   let panel = controller.panel!
   panel.appearance = app.appearance
   panel.animationBehavior = .none
   panel.setContentSize(NSSize(width: width, height: panel.contentView!.bounds.height))
   panel.orderBack(nil)
   let content = panel.contentView!
   content.layoutSubtreeIfNeeded()
   panel.display()
   let name = "choices-\(count)-width-\(Int(width))-\(theme.rawValue)"
   let frames = controller.cards.map { $0.convert($0.bounds, to: content) }
   let heights = frames.map { $0.height }
   let spread = heights.max()! - heights.min()!
   let naturalHeights = controller.cards.map { card -> CGFloat in
    let stack = card.subviews.compactMap { $0 as? NSStackView }.first!
    let heights = stack.arrangedSubviews.map { view -> CGFloat in
     if let text = view as? RichTextLabel, let manager = text.layoutManager, let container = text.textContainer {
      manager.ensureLayout(for: container)
      return ceil(manager.usedRect(for: container).height)
     }
     if let field = view as? NSTextField { return field.cell!.cellSize(forBounds: field.bounds).height }
     return view.fittingSize.height
    }
    return heights.reduce(0, +) + stack.spacing * CGFloat(max(0, heights.count - 1)) + 22
   }
   let tallestContent = naturalHeights.max()!
   if heights.contains(where: { abs($0 - tallestContent) > 0.5 }) { failures.append("\(name): row should fit tallest content \(tallestContent), got \(heights)") }
   var clipped: [String] = []
   var titles: [CGFloat] = []
   var lineCounts: [Int] = []
   for card in controller.cards {
    if !content.bounds.contains(card.convert(card.bounds, to: content)) { clipped.append("card") }
    for view in descendants(card) {
     if let text = view as? RichTextLabel, let manager = text.layoutManager, let container = text.textContainer {
      manager.ensureLayout(for: container)
      let used = manager.usedRect(for: container)
      if used.height > text.bounds.height + 0.5 || used.width > text.bounds.width + 0.5 { clipped.append("description: \(text.string)") }
      var lines = 0
      manager.enumerateLineFragments(forGlyphRange: NSRange(location: 0, length: manager.numberOfGlyphs)) { _, _, _, _, _ in lines += 1 }
      lineCounts.append(lines)
     }
     if let field = view as? NSTextField, field.stringValue == card.option.label {
      titles.append(card.bounds.maxY - field.convert(field.bounds, to: card).maxY)
      if let cell = field.cell, cell.cellSize(forBounds: field.bounds).height > field.bounds.height + 0.5 { clipped.append("title") }
     }
    }
   }
   if spread > 0.5 { failures.append("\(name): unequal heights \(heights)") }
   if titles.contains(where: { abs($0 - 11) > 0.5 }) { failures.append("\(name): titles not top aligned \(titles)") }
   if !clipped.isEmpty { failures.append("\(name): clipped \(clipped)") }
   if Set(frames.map { $0.midY }).count != 1 { failures.append("\(name): cards left the horizontal row") }
   if !content.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds, to: content)) { failures.append("\(name): submit outside window") }
   // Exercise the real click transition after changing layout.
   controller.cardClicked(controller.cards[count - 1])
   if controller.checkedIds() != ["choice-\(count - 1)"] { failures.append("\(name): selection changed") }
   if capture {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
    process.arguments = ["-x", "-o", "-l", String(panel.windowNumber), root + "/" + name + ".png"]
    try process.run(); process.waitUntilExit()
    if process.terminationStatus != 0 { failures.append("\(name): screencapture failed") }
   }
   metrics.append(["case": name, "width": width, "count": count, "theme": theme.rawValue, "heights": heights, "heightSpread": spread, "naturalHeights": naturalHeights, "tallestContent": tallestContent, "titleTopInsets": titles, "descriptionLines": lineCounts, "clipped": clipped, "windowNumber": panel.windowNumber, "frames": frames.map { ["x": $0.minX, "y": $0.minY, "width": $0.width, "height": $0.height] }])
   controller.close()
  }
 }
}
try JSONSerialization.data(withJSONObject: metrics, options: [.prettyPrinted, .sortedKeys]).write(to: URL(fileURLWithPath: root + "/metrics.json"))
for failure in failures { print("FAIL \(failure)") }
if !recordOnly && !failures.isEmpty { exit(1) }
print("OPTION_HEIGHT_\(recordOnly ? "BEFORE" : "PASS") cases=\(metrics.count) failures=\(failures.count)")
