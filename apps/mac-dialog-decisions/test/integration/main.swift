import AppKit
import Foundation

let app = NSApplication.shared
let host = DialogHost(directory:nil)
app.delegate = host
app.setActivationPolicy(.accessory)
app.appearance = NSAppearance(named:.darkAqua)
let root=CommandLine.arguments[1]
let source=CommandLine.arguments[2]
let specPath=root+"/integration-panel.json"
let spec:[String:Any] = ["window_title":"統合: 前面化と画像添付", "title":"前面化・リンク・画像・添付 https://example.com", "body":"画像: "+source, "options":[["id":"yes","label":"確認した","description":"https://example.com/option"]], "allow_text":true,"multi_select":false,"draft_directory":root]
try JSONSerialization.data(withJSONObject:spec).write(to:URL(fileURLWithPath:specPath))
func waitFor(_ name:String, condition:@escaping ()->Bool, done:@escaping ()->Void) {
 let deadline=Date(timeIntervalSinceNow:5)
 Timer.scheduledTimer(withTimeInterval:0.01,repeats:true) { timer in
  if condition() {timer.invalidate();done();return}
  if Date()>deadline {fputs("integration timeout: "+name+"\n",stderr);exit(1)}
 }
}
func finish(_ controller:DialogController) {
 let panel=controller.panel!,view=panel.contentView!
 for width in [720.0,520.0] {
  panel.setContentSize(NSSize(width:width,height:panel.contentLayoutRect.height));view.layoutSubtreeIfNeeded()
  precondition(view.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:view)),"combined buttons remain inside window")
 }
 panel.setContentSize(NSSize(width:720,height:panel.contentLayoutRect.height));view.layoutSubtreeIfNeeded()
 let capture=Process();capture.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture");capture.arguments=["-x","-l",String(panel.windowNumber),root+"/panel-combined.png"]
 try! capture.run();capture.waitUntilExit();precondition(capture.terminationStatus==0)
 print("INTEGRATED_PANEL_PASS key=1 main=1 normal=1 links=1 images=1 attachments=1 selection=0 sent=0 buttonsInside=1")
 host.closeAll();exit(0)
}
let startTimer=Timer.scheduledTimer(withTimeInterval:0.2,repeats:false) { _ in
 let frontBefore=NSWorkspace.shared.frontmostApplication?.processIdentifier
 host.open(specPath)
 let controller=host.windows.values.first!,panel=host.windows.values.first!.panel!
 precondition(frontBefore==NSWorkspace.shared.frontmostApplication?.processIdentifier && !panel.isKeyWindow,"initial display must not steal focus")
 precondition(panel.canBecomeMain && !panel.styleMask.contains(.nonactivatingPanel) && panel.level == .normal)
 controller.respond(["status":"rich_card","title":"公開ページ","url":"https://example.com","site":"example.com","path":source])
 controller.respond(["status":"rich_image","path":source])
 controller.attachFiles([URL(fileURLWithPath:source)])
 waitFor("real image and attachment",condition:{controller.attachments.count==1 && controller.attachmentLoading==0 && controller.imageWork.operationCount==0}) {
  precondition(controller.checkedIds().isEmpty && !controller.answerPending)
  precondition(richLinkRanges(controller.spec.title).count==1)
  _ = host.applicationShouldHandleReopen(app,hasVisibleWindows:true)
  waitFor("selected rich panel becomes foreground/key/main",condition:{panel.isVisible && panel.isKeyWindow && panel.isMainWindow && NSWorkspace.shared.frontmostApplication?.processIdentifier==ProcessInfo.processInfo.processIdentifier}) {finish(controller)}
 }
}
app.run()
