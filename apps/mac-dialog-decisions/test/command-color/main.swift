import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root = CommandLine.arguments[1]
let body = "ブロック理由：削除操作は確認が必要です（規則: rm-paths）\n対象コマンド：rm \\\n\n  \"$TARGET\"\n\nコマンド: bash /tmp/check.sh https://example.com/command\n\n対象プロジェクト: パネルの表示試験\n必要な理由: コマンドの赤表示を確認します"
let data = try JSONSerialization.data(withJSONObject: ["window_title":"ikesu 表示試験", "kind":"command_guard", "title":"実行確認: rm", "body":body, "options":[["id":"approve","label":"承認"],["id":"reject","label":"却下"]], "allow_text":true, "multi_select":false, "draft_directory":root])
let spec = try JSONDecoder().decode(DialogSpec.self, from:data)
for name in [NSAppearance.Name.aqua, .darkAqua] {
  app.appearance = NSAppearance(named:name)
  let controller=DialogController(spec:spec,specPath:root+"/spec.json")
  controller.build()
  controller.panel.appearance=app.appearance
  controller.panel.orderBack(nil)
  RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
  let view=controller.panel.contentView!
  view.layoutSubtreeIfNeeded()
  func fields(_ view:NSView)->[NSTextField] { view.subviews.flatMap { child in (child as? NSTextField).map {[$0]} ?? fields(child) } }
  let labels=fields(view)
  let commandLabels=labels.filter {$0.stringValue.contains("対象コマンド：") || $0.stringValue.contains("コマンド: ")}
  precondition(commandLabels.count==2,"both body portions must render")
  for field in commandLabels {
    precondition(field.isSelectable)
    let attributed=field.attributedStringValue
    let location=(body as NSString).range(of:field.stringValue)
    precondition(location.location != NSNotFound)
    let globalRanges=commandBodyRanges(body)
    for n in 0..<(field.stringValue as NSString).length {
      let expected=globalRanges.contains {NSLocationInRange(location.location+n,$0)}
      let red=attributed.attribute(.foregroundColor,at:n,effectiveRange:nil) as? NSColor == .systemRed
      precondition(red==expected,"production body split red range mismatch")
    }
  }
  precondition(labels.contains {$0.stringValue.contains("対象プロジェクト: パネルの表示試験")})
  let capture=Process()
  capture.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture")
  capture.arguments=["-x","-l",String(controller.panel.windowNumber),root+"/panel-"+name.rawValue+".png"]
  try capture.run();capture.waitUntilExit();precondition(capture.terminationStatus==0)
  print("PANEL_PASS theme=\(name.rawValue) commandLabels=\(commandLabels.count) window=\(controller.panel.windowNumber)")
  controller.close()
}
