import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let rows=try JSONSerialization.jsonObject(with:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1]))) as! [[String:Any]]
let root=CommandLine.arguments[2]
func fields(_ view:NSView)->[RichTextLabel] {view.subviews.flatMap {($0 as? RichTextLabel).map {[$0]} ?? fields($0)}}
for row in rows {
 let name=row["name"] as! String
 let decision=row["decision"] as! [String:Any]
 let origin=decision["origin"] as! [String:Any]
 let body=decision["body"] as! String
 let spec=try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:row["spec"]!))
 // Expectations come from the actual guard decision, never a candidate list
 // injected into the production renderer.
 var expected:[NSRange]=[]
 for (heading,key) in [("対象コマンド：","target_command"),("コマンド: ","command")] {
  if let command=origin[key] as? String, !command.isEmpty {
   let full=(body as NSString).range(of:heading+command)
   precondition(full.location != NSNotFound)
   expected.append(NSRange(location:full.location+(heading as NSString).length,length:(command as NSString).length))
  }
 }
 precondition(expected.count == (name=="same" ? 1:2))
 for theme in [NSAppearance.Name.aqua,.darkAqua] {
  app.appearance=NSAppearance(named:theme)
  let controller=DialogController(spec:spec,specPath:root+"/"+name+".json")
  controller.build();controller.panel.appearance=app.appearance;controller.panel.orderBack(nil);controller.panel.display()
  RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
  let content=controller.panel.contentView!
  content.wantsLayer=true
  controller.panel.effectiveAppearance.performAsCurrentDrawingAppearance {content.layer!.backgroundColor=NSColor.windowBackgroundColor.cgColor}
  content.layoutSubtreeIfNeeded()
  let labels=fields(content)
  let bodyLabels=labels.filter {$0.string.contains("対象コマンド：") || $0.string.contains("コマンド: ")}
  precondition(bodyLabels.count == (name=="same" ? 1:2),"duplicate command detail")
  var redCharacters=0
  for label in bodyLabels {
   let offset=(body as NSString).range(of:label.string).location
   precondition(offset != NSNotFound)
   let attributed=label.attributedString()
   for index in 0..<(label.string as NSString).length {
    let red=attributed.attribute(.foregroundColor,at:index,effectiveRange:nil) as? NSColor == .systemRed
    let wanted=expected.contains {NSLocationInRange(offset+index,$0)}
    precondition(red==wanted,"red range mismatch case=\(name) index=\(index)")
    if wanted {precondition((attributed.attribute(.font,at:index,effectiveRange:nil) as! NSFont).isFixedPitch);redCharacters+=1}
   }
   precondition(label.isSelectable && !label.isEditable)
   if let url=label.string.range(of:"https://example.test/help") {
    let location=NSRange(url,in:label.string).location
    precondition(attributed.attribute(.link,at:location,effectiveRange:nil) != nil)
    precondition(attributed.attribute(.foregroundColor,at:location,effectiveRange:nil) as? NSColor != .systemRed)
   }
  }
  precondition(redCharacters==expected.reduce(0){$0+$1.length})
  let title=labels.first {$0.string==spec.title}!
  precondition(title.string==decision["title"] as! String)
  for index in 0..<(title.string as NSString).length {precondition(title.attributedString().attribute(.foregroundColor,at:index,effectiveRange:nil) as? NSColor != .systemRed)}
  for card in controller.cards {precondition(content.bounds.contains(card.convert(card.bounds,to:content)))}
  precondition(content.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:content)))
  print("WINDOW case=\(name) number=\(controller.panel.windowNumber) visible=\(controller.panel.isVisible) frame=\(controller.panel.frame)")
  let file=root+"/"+name+"-"+theme.rawValue+".png"
  // Capture this actual AppKit window's content directly. This needs no
  // permission to read other windows or the user's desktop.
  let bitmap=content.bitmapImageRepForCachingDisplay(in:content.bounds)!
  content.cacheDisplay(in:content.bounds,to:bitmap)
  try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:file))
  var redPixels=0
  for x in 0..<bitmap.pixelsWide {for y in 0..<bitmap.pixelsHigh {
   if let color=bitmap.colorAt(x:x,y:y)?.usingColorSpace(.deviceRGB),color.redComponent>color.greenComponent && color.redComponent>color.blueComponent {redPixels+=1}
  }}
  precondition(redPixels>0)
  print("PASS case=\(name) theme=\(theme.rawValue) commandRanges=\(expected.count) exactRedUTF16=\(redCharacters) redPixels=\(redPixels) selectable=1 links=preserved")
  controller.close()
 }
}
