import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let root = CommandLine.arguments[1]
let body = "ブロック理由：削除操作は確認が必要です（規則: rm-paths）\n対象コマンド：rm \\\n  \"$TARGET\"\n\nコマンド: bash /tmp/check.sh https://example.com/command\n\n対象プロジェクト: パネルの表示試験\n必要な理由: コマンドの赤表示を確認します"
let data = try JSONSerialization.data(withJSONObject: ["window_title":"ikesu 表示試験", "kind":"command_guard", "title":"実行確認: rm", "body":body, "options":[["id":"approve","label":"承認"],["id":"reject","label":"却下"]], "allow_text":true, "multi_select":false, "draft_directory":root])
let spec = try JSONDecoder().decode(DialogSpec.self, from:data)
for name in [NSAppearance.Name.aqua, .darkAqua] {
  app.appearance = NSAppearance(named:name)
  let controller=DialogController(spec:spec,specPath:root+"/spec.json")
  controller.build()
  controller.panel.appearance=app.appearance
  let view=controller.panel.contentView!
  view.layoutSubtreeIfNeeded()
  func fields(_ view:NSView)->[NSTextField] { view.subviews.flatMap { child in (child as? NSTextField).map {[$0]} ?? fields(child) } }
  let labels=fields(view)
  let commandLabels=labels.filter {$0.stringValue.contains("$TARGET") || $0.stringValue.contains("コマンド: ")}
  precondition(commandLabels.count==2,"both body portions must render")
  for field in commandLabels {
    precondition(field.isSelectable)
    let attributed=field.attributedStringValue
    let ranges=commandBodyRanges(field.stringValue)
    for range in ranges {
      for n in range.location..<NSMaxRange(range) {
        precondition(attributed.attribute(.foregroundColor,at:n,effectiveRange:nil) as? NSColor == .systemRed)
      }
    }
  }
  precondition(labels.contains {$0.stringValue.contains("対象プロジェクト: パネルの表示試験")})
  let rep=view.bitmapImageRepForCachingDisplay(in:view.bounds)!
  view.cacheDisplay(in:view.bounds,to:rep)
  try rep.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:root+"/panel-"+name.rawValue+".png"))
  print("PANEL_PASS theme=\(name.rawValue) commandLabels=\(commandLabels.count) width=\(rep.pixelsWide) height=\(rep.pixelsHigh)")
  controller.close()
}
