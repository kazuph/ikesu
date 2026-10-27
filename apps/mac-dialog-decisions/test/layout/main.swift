import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.appearance = NSAppearance(named: .darkAqua)
let baseline = CommandLine.arguments.contains("--baseline")
let spec = try JSONDecoder().decode(DialogSpec.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
for separator in [" ", "\t", "\n", " \t\n"] {
 for colon in [":", "："] {
  let original = "対象プロジェクト\(colon) ikesu。\(separator)必要な理由\(colon) 表示確認。\(separator)実施内容\(colon) 検証。\(separator)確認済み\(colon) 差分。\(separator)戻し方\(colon) 中止。"
  let formatted = "対象プロジェクト: ikesu。\n必要な理由: 表示確認。\n実施内容: 検証。\n確認済み: 差分。\n戻し方: 中止。"
  precondition(comparableApprovalDetail(original) == comparableApprovalDetail(formatted), "field boundary normalization differs")
 }
}
precondition(comparableApprovalDetail("必要な理由: A B") != comparableApprovalDetail("必要な理由: AB"), "whitespace within fields must be preserved")
let controller = DialogController(spec: spec, specPath: CommandLine.arguments[1])
DispatchQueue.main.async {
 controller.build()
 controller.show()
}
DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
 let root = controller.panel.contentView!
 root.layoutSubtreeIfNeeded()
 let capture = Process()
 capture.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
 capture.arguments = ["-x", "-l", String(controller.panel.windowNumber), CommandLine.arguments[2]]
 do { try capture.run(); capture.waitUntilExit() } catch { fatalError("screencapture failed: \(error)") }
 precondition(capture.terminationStatus == 0, "actual window capture failed")
 print("screencapture window=\(controller.panel.windowNumber) appearance=\(controller.panel.effectiveAppearance.name.rawValue)")
 var fields: [NSTextField] = []
 func collect(_ view: NSView) {
  if let field = view as? NSTextField { fields.append(field) }
  for child in view.subviews { collect(child) }
 }
 collect(root)
 if !baseline {
 for field in fields where field.stringValue.contains("対象プロジェクト:") || field.stringValue.hasPrefix("承認が必要な理由:") {
  precondition(root.bounds.contains(field.convert(field.bounds, to: root)), "body must fit the window")
  var parent = field.superview
  while let view = parent {
   if let clip = view as? NSClipView {
    precondition(clip.bounds.contains(field.convert(field.bounds, to: clip)), "body must not be clipped")
   }
   parent = view.superview
  }
 }
 if CommandLine.arguments.contains("--field-count") {
  let lines=fields.map {$0.stringValue}.joined(separator:"\n").components(separatedBy:"\n")
  for name in ["対象プロジェクト", "必要な理由", "実施内容", "確認済み", "戻し方"] {
   let count=lines.filter {$0.hasPrefix(name+":") || $0.hasPrefix(name+"：")}.count
   precondition(count == 1, "request field must appear exactly once: \(name)=\(count)")
  }
  print("five request fields each rendered exactly once")
 }
 print("requested body labels fully inside window and scroll viewport")
 precondition(!fields.contains { field in ["コマンド：", "理由：", "場所："].contains(where: field.stringValue.hasPrefix) }, "duplicate Origin details")
 if spec.body?.hasPrefix("承認が必要な理由:") == true {
  precondition(fields.contains { $0.stringValue.hasPrefix("承認が必要な理由:") && $0.textColor == Look.accent }, "guard trigger must be emphasized")
 }
 precondition(fields.contains { $0.stringValue == spec.title && $0.maximumNumberOfLines == 1 && $0.lineBreakMode == .byTruncatingTail }, "long guard title must truncate")
 }
 var results: [[String:Any]] = []
 for card in controller.cards {
  let rect = card.convert(card.bounds, to: root)
  var visible = root.bounds.contains(rect)
  var parent = card.superview
  while let view = parent {
   if let clip = view as? NSClipView {
    visible = visible && clip.bounds.contains(card.convert(card.bounds, to: clip))
   }
   parent = view.superview
  }
  results.append(["id":card.option.id,"visible":visible,"rect":[rect.minX,rect.minY,rect.width,rect.height]])
 }
 let data = try! JSONSerialization.data(withJSONObject:["cards":results,"panel":[root.bounds.width,root.bounds.height]], options:[.sortedKeys])
 print(String(data:data,encoding:.utf8)!)
 for card in controller.cards {
  controller.cardClicked(card)
  precondition(!controller.answerPending, "selection must not submit")
  precondition(controller.checkedIds() == [card.option.id])
  controller.cardClicked(card)
  precondition(controller.checkedIds().isEmpty, "clicking again must clear selection")
 }
 if !baseline {
 let chromeHeight = controller.panel.frame.height - controller.panel.contentLayoutRect.height
 controller.panel.setContentSize(NSSize(width: controller.panel.minSize.width, height: controller.panel.minSize.height - chromeHeight))
 root.layoutSubtreeIfNeeded()
 for card in controller.cards {
  let rect = card.convert(card.bounds, to: root)
  precondition(root.bounds.contains(rect), "choice must remain visible at minimum window size")
 }
 print("minimum-size choices visible; origin duplicates absent; trigger emphasized; selection never submits")
 }
 controller.panel.orderOut(nil)
 exit(results.allSatisfy { $0["visible"] as? Bool == true } ? 0 : 1)
}
app.run()
