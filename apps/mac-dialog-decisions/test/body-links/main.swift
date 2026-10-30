import AppKit
import Foundation

let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root=CommandLine.arguments[1]
let image=root+"/image.png", video=root+"/video.mp4", website="https://example.com/link"
let sources=[website,image,video]
let body=sources.joined(separator:"\n")
let input:[String:Any]=["window_title":"本文リンク試験", "title":"リンク", "body":body,
  "options":[],"allow_text":false,"multi_select":false,"media_sources":sources]
let spec=try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:input))
let controller=DialogController(spec:spec,specPath:root+"/spec.json")
var opened:[URL]=[]
controller.openLink={ opened.append($0); return true }
func labels(_ view:NSView)->[RichTextLabel] {
  view.subviews.flatMap { child in (child as? RichTextLabel).map {[$0]} ?? labels(child) }
}
DispatchQueue.main.async {
  controller.build(); controller.show()
  let panel=controller.panel!, content=panel.contentView!
  content.layoutSubtreeIfNeeded()
  let text=labels(content).first {$0.string==body}!
  func click(_ index:Int) {
    if index==sources.count+1 { controller.close();exit(0) }
    let source=index<sources.count ? sources[index] : website
    let target=index<sources.count ? text : labels(controller.mediaCards[website]!).first {$0.string==website}!
    panel.resignKey(); NSApp.deactivate()
    precondition(!panel.isKeyWindow && !NSApp.isActive,"click must start with panel behind")
    let range=(target.string as NSString).range(of:source)
    let manager=target.layoutManager!, container=target.textContainer!
    manager.ensureLayout(for:container)
    let glyph=manager.glyphRange(forCharacterRange:NSRange(location:range.location,length:1),actualCharacterRange:nil)
    let rect=manager.boundingRect(forGlyphRange:glyph,in:container)
    let point=target.convert(NSPoint(x:rect.midX+target.textContainerOrigin.x,y:rect.midY+target.textContainerOrigin.y),to:nil)
    let hit=content.hitTest(content.convert(point,from:nil))!
    precondition(hit===target && hit.acceptsFirstMouse(for:nil),"link must accept first mouse")
    let down=NSEvent.mouseEvent(with:.leftMouseDown,location:point,modifierFlags:[],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,eventNumber:index*2+1,clickCount:1,pressure:1)!
    let up=NSEvent.mouseEvent(with:.leftMouseUp,location:point,modifierFlags:[],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:panel.windowNumber,context:nil,eventNumber:index*2+2,clickCount:1,pressure:0)!
    NSApp.postEvent(down,atStart:false);NSApp.postEvent(up,atStart:false)
    DispatchQueue.main.asyncAfter(deadline:.now()+0.2) {
      let expected=source.hasPrefix("/") ? URL(fileURLWithPath:source) : URL(string:source)!
      guard opened.last==expected && opened.count==index+1 else {
        fputs("FAIL actual click source=\(source) opened=\(opened)\n",stderr); exit(1)
      }
      print("BODY_LINK_CLICK_PASS source=\(source)")
      click(index+1)
    }
  }
  click(0)
}
app.run()
