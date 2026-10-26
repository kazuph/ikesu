import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.appearance=NSAppearance(named:.darkAqua)
let spec=try JSONDecoder().decode(DialogSpec.self,from:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1])))
let controller=DialogController(spec:spec,specPath:CommandLine.arguments[1])
controller.build()
controller.show()
DispatchQueue.main.asyncAfter(deadline:.now()+0.5) {
 let panel=controller.panel!
 let root=panel.contentView!
 root.layoutSubtreeIfNeeded()
 print("before-test active=\(NSApp.isActive) key=\(panel.isKeyWindow)")
 panel.resignKey()
 NSApp.deactivate()
 print("after-deactivate active=\(NSApp.isActive) key=\(panel.isKeyWindow)")
 precondition(!panel.isKeyWindow && !NSApp.isActive,"must start with inactive app and non-key panel")
 var failure=false
 for card in controller.cards {
  let labels=card.subviews.flatMap { $0 is NSStackView ? $0.subviews : [$0] }.compactMap {$0 as? NSTextField}.filter{!$0.stringValue.isEmpty}
  precondition(!labels.isEmpty)
  let title=labels[0]
  let windowPoint=title.convert(NSPoint(x:title.bounds.midX,y:title.bounds.midY),to:nil)
  let event=NSEvent.mouseEvent(with:.leftMouseDown,location:windowPoint,modifierFlags:[],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,eventNumber:1,clickCount:1,pressure:1)!
  let hit=root.hitTest(root.convert(windowPoint,from:nil))!
  let accepted=hit.acceptsFirstMouse(for:event)
  print("id=\(card.option.id) hit=\(type(of:hit)) acceptsFirstMouse=\(hit.acceptsFirstMouse(for:event)) needsKey=\(hit.needsPanelToBecomeKey)")
  failure = failure || !accepted
  for image in card.subviews.compactMap({$0 as? NSImageView}) {
   let radioPoint=image.convert(NSPoint(x:image.bounds.midX,y:image.bounds.midY),to:nil)
   let radioHit=root.hitTest(root.convert(radioPoint,from:nil))!
   print("id=\(card.option.id) radioHit=\(type(of:radioHit)) acceptsFirstMouse=\(radioHit.acceptsFirstMouse(for:event))")
   failure = failure || !radioHit.acceptsFirstMouse(for:event)
  }
 }
 print("first-event hit acceptance failure=\(failure)")
 if failure {exit(1)}
 let choiceArg=CommandLine.arguments.first(where:{$0.hasPrefix("--choice=")}) ?? "--choice=0"
 let index=Int(choiceArg.dropFirst("--choice=".count))!
 let card=controller.cards[index]
 let title=(card.subviews.compactMap {$0 as? NSStackView}.first!).arrangedSubviews[0]
 let radio=card.subviews.compactMap {$0 as? NSImageView}.first!
 let target: NSView = CommandLine.arguments.contains("--title") ? title : radio
 let point=target.convert(NSPoint(x:target.bounds.midX,y:target.bounds.midY),to:nil)
 for type in [NSEvent.EventType.leftMouseDown,.leftMouseUp] {
  let event=NSEvent.mouseEvent(with:type,location:point,modifierFlags:[],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,eventNumber:1,clickCount:1,pressure:type == .leftMouseDown ? 1 : 0)!
  panel.sendEvent(event)
 }
 precondition(controller.checkedIds()==[card.option.id],"first click must select")
 precondition(!controller.answerPending,"selection must not send")
 print("inactive first click selected; answerPending=false")
 for n in 1...controller.cards.count {
  let event=NSEvent.keyEvent(with:.keyDown,location:.zero,modifierFlags:[.command],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,characters:String(n),charactersIgnoringModifiers:String(n),isARepeat:false,keyCode:0)!
  let id=controller.cards[n-1].option.id
  let expected = controller.checkedIds()==[id] ? [] : [id]
  _=controller.handleKey(event)
  precondition(!controller.answerPending,"keyboard selection must not send")
  precondition(controller.checkedIds()==expected,"command-number selection changed")
 }
 print("command-number selections preserved; no automatic submit")
 panel.orderOut(nil)
 exit(0)
}
app.run()
