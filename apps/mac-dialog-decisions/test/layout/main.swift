import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let spec = try JSONDecoder().decode(DialogSpec.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
let controller = DialogController(spec: spec, specPath: CommandLine.arguments[1])
controller.build()
controller.show()
DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
 let root = controller.panel.contentView!
 root.layoutSubtreeIfNeeded()
 let bitmap = root.bitmapImageRepForCachingDisplay(in: root.bounds)!
 root.cacheDisplay(in: root.bounds, to: bitmap)
 try! bitmap.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: CommandLine.arguments[2]))
 var fields: [NSTextField] = []
 func collect(_ view: NSView) {
  if let field = view as? NSTextField { fields.append(field) }
  for child in view.subviews { collect(child) }
 }
 collect(root)
 precondition(!fields.contains { field in ["コマンド：", "理由：", "場所："].contains(where: field.stringValue.hasPrefix) }, "duplicate Origin details")
 if spec.body?.hasPrefix("承認が必要な理由:") == true {
  precondition(fields.contains { $0.stringValue.hasPrefix("承認が必要な理由:") && $0.textColor == Look.accent }, "guard trigger must be emphasized")
 }
 precondition(fields.contains { $0.stringValue == spec.title && $0.maximumNumberOfLines == 1 && $0.lineBreakMode == .byTruncatingTail }, "long guard title must truncate")
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
 let chromeHeight = controller.panel.frame.height - controller.panel.contentLayoutRect.height
 controller.panel.setContentSize(NSSize(width: controller.panel.minSize.width, height: controller.panel.minSize.height - chromeHeight))
 root.layoutSubtreeIfNeeded()
 for card in controller.cards {
  let rect = card.convert(card.bounds, to: root)
  precondition(root.bounds.contains(rect), "choice must remain visible at minimum window size")
 }
 print("minimum-size choices visible; origin duplicates absent; trigger emphasized; selection never submits")
 controller.panel.orderOut(nil)
 exit(results.allSatisfy { $0["visible"] as? Bool == true } ? 0 : 1)
}
app.run()
