import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root = CommandLine.arguments[1]
let command="rm \\\n\n  \"$TARGET\" https://example.com/command"
let body = "ブロック理由：削除操作は確認が必要です（規則: rm-paths）\n対象コマンド："+command+"\n\nコマンド: "+command+"\n\n検証用の送信です https://example.com/help"
let data = try JSONSerialization.data(withJSONObject: ["window_title":"ikesu 表示試験", "kind":"command_guard", "title":"実行確認: rm", "body":body, "details":[["label":"コマンド","value":command]], "options":[["id":"approve","label":"承認"],["id":"reject","label":"却下"]], "allow_text":true, "multi_select":false, "draft_directory":root])
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
  func fields(_ view:NSView)->[RichTextLabel] { view.subviews.flatMap { child in (child as? RichTextLabel).map {[$0]} ?? fields(child) } }
  let labels=fields(view)
  let commandLabels=labels.filter {$0.string.contains("対象コマンド：") || $0.string.contains("コマンド: ")}
  precondition(commandLabels.count==2,"both body portions must render")
  for field in commandLabels {
    precondition(field.isSelectable)
    let attributed=field.attributedString()
    let location=(body as NSString).range(of:field.string)
    precondition(location.location != NSNotFound)
    let globalRanges=commandBodyRanges(body,commands:[command])
    for n in 0..<(field.string as NSString).length {
      let expected=globalRanges.contains {NSLocationInRange(location.location+n,$0)}
      let red=attributed.attribute(.foregroundColor,at:n,effectiveRange:nil) as? NSColor == .systemRed
      precondition(red==expected,"production body split red range mismatch")
    }
  }
  precondition(labels.contains {$0.string.contains("検証用の送信です https://example.com/help")})
  for field in commandLabels {
    let text=field.string as NSString
    let link=text.range(of:"https://example.com/command")
    if link.location != NSNotFound { precondition(field.attributedString().attribute(.link,at:link.location,effectiveRange:nil) != nil) }
  }
  let capture=Process()
  capture.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture")
  capture.arguments=["-x","-l",String(controller.panel.windowNumber),root+"/panel-"+name.rawValue+".png"]
  try capture.run();capture.waitUntilExit();precondition(capture.terminationStatus==0)
  print("PANEL_PASS theme=\(name.rawValue) commandLabels=\(commandLabels.count) window=\(controller.panel.windowNumber)")
  controller.close()
  let url="https://example.com/command"
  let probe=NSPanel(contentRect:NSRect(x:40,y:40,width:440,height:50),styleMask:[.borderless],backing:.buffered,defer:false)
  probe.appearance=app.appearance
  probe.backgroundColor=NSColor.textBackgroundColor
  let field=richLabel(url,size:15,commandRanges:[NSRange(location:0,length:(url as NSString).length)])
  field.frame=NSRect(x:10,y:10,width:420,height:30)
  probe.contentView!.addSubview(field);probe.orderBack(nil)
  RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
  let file=root+"/link-"+name.rawValue+".png"
  let shot=Process();shot.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture");shot.arguments=["-x","-o","-l",String(probe.windowNumber),file]
  try shot.run();shot.waitUntilExit();precondition(shot.terminationStatus==0)
  let pixels=NSBitmapImageRep(data:try Data(contentsOf:URL(fileURLWithPath:file)))!
  var red=0,blue=0
  for x in 0..<pixels.pixelsWide {for y in 0..<pixels.pixelsHigh {
    if let c=pixels.colorAt(x:x,y:y)?.usingColorSpace(.deviceRGB),c.alphaComponent>0 {
      if c.redComponent>c.greenComponent && c.redComponent>c.blueComponent {red+=1}
      if c.blueComponent>c.redComponent && c.blueComponent>c.greenComponent {blue+=1}
    }
  }}
  precondition(red>0 && blue==0,"native command link must render red")
  precondition(field.attributedString().attribute(.link,at:0,effectiveRange:nil) != nil)
  print("LINK_PIXELS theme=\(name.rawValue) red=\(red) blue=\(blue) nativeLink=1")
  probe.close()
}

let longTitle=String(repeating:"長い見出しを切らずに折り返して表示する試験です。",count:30)+" https://example.test/full/path"
var longObject=try JSONSerialization.jsonObject(with:data) as! [String:Any]
longObject["title"]=longTitle
let longSpec=try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:longObject))
let longController=DialogController(spec:longSpec,specPath:root+"/long-title.json")
longController.build();longController.panel.orderBack(nil)
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
let content=longController.panel.contentView!
content.layoutSubtreeIfNeeded()
func richViews(_ view:NSView)->[RichTextLabel] {view.subviews.flatMap {($0 as? RichTextLabel).map {[$0]} ?? richViews($0)}}
precondition(richViews(content).contains {$0.string==longTitle},"complete heading must remain in the view")
for card in longController.cards {
 precondition(content.bounds.contains(card.convert(card.bounds,to:content)),"long title pushed buttons outside window")
 var ancestor=card.superview
 while let parent=ancestor {
  if let clip=parent as? NSClipView {precondition(clip.bounds.contains(card.convert(card.bounds,to:clip)),"choice clipped with long title")}
  ancestor=parent.superview
 }
}
precondition(content.bounds.contains(longController.submitButton.convert(longController.submitButton.bounds,to:content)),"submit outside window")
let longShot=Process();longShot.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture");longShot.arguments=["-x","-l",String(longController.panel.windowNumber),root+"/long-title-panel.png"]
try longShot.run();longShot.waitUntilExit();precondition(longShot.terminationStatus==0)
print("LONG_TITLE_PASS characters=\(longTitle.count) choicesVisible=\(longController.cards.count) submitVisible=1")
longController.close()
