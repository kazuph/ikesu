import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let rows=try JSONSerialization.jsonObject(with:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1]))) as! [[String:Any]]
let root=CommandLine.arguments[2]
func fields(_ view:NSView)->[RichTextLabel] {view.subviews.flatMap {($0 as? RichTextLabel).map {[$0]} ?? fields($0)}}
func textFields(_ view:NSView)->[NSTextField] {view.subviews.flatMap {($0 as? NSTextField).map {[$0]} ?? textFields($0)}}
for row in rows {
 let name=row["name"] as! String
 let decision=row["decision"] as! [String:Any]
 let origin=decision["origin"] as! [String:Any]
 let body=decision["body"] as! String
 let spec=try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:row["spec"]!))
 let command=origin["command"] as! String
 let expected: NSRange? = name == "requested-example" ? NSRange(location:0,length:143) : name == "same" ? NSRange(location:0,length:23) : nil
 let actual = spec.approval_target_range.map { NSRange(location:$0.start,length:$0.length) }
 precondition(actual == expected,"independent expected range mismatch for \(name)")
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
  let bodyLabels=labels.filter {$0.string==command}
  precondition(bodyLabels.count == 1,"full command must appear once")
  var redCharacters=0
  for label in bodyLabels {
   let attributed=label.attributedString()
   for index in 0..<(label.string as NSString).length {
    let red=attributed.attribute(.foregroundColor,at:index,effectiveRange:nil) as? NSColor == .systemRed
    let wanted=expected.map {NSLocationInRange(index,$0)} ?? false
    precondition(red==wanted,"red range mismatch case=\(name) index=\(index)")
    if wanted {precondition((attributed.attribute(.font,at:index,effectiveRange:nil) as! NSFont).isFixedPitch);redCharacters+=1}
   }
   precondition(label.isSelectable && !label.isEditable)
   if let url=label.string.range(of:"https://example.test/command") {
    let location=NSRange(url,in:label.string).location
    precondition(attributed.attribute(.link,at:location,effectiveRange:nil) != nil)
   }
  }
  precondition(redCharacters == (expected?.length ?? 0))
  if name=="quoted-command" {
   let quote="対象コマンド：/bin/rm /outside/victim"
   let quoted=(body as NSString).range(of:quote,options:.backwards)
   precondition(quoted.location != NSNotFound)
   precondition(labels.contains {$0.string.contains(quote)},"reason quote must remain visible")
  }
  let title=labels.first {$0.string==spec.title}!
  precondition(title.string==decision["title"] as! String)
  for index in 0..<(title.string as NSString).length {precondition(title.attributedString().attribute(.foregroundColor,at:index,effectiveRange:nil) as? NSColor != .systemRed)}
  if name == "requested-example" {
   let commandLabel=bodyLabels[0]
   let firstField=labels.first {$0.string.contains("TenkaiHifumi のメモリー")}!
   precondition(title.convert(title.bounds,to:content).midY > commandLabel.convert(commandLabel.bounds,to:content).midY)
   precondition(commandLabel.convert(commandLabel.bounds,to:content).midY > firstField.convert(firstField.bounds,to:content).midY)
   let names=textFields(content).map(\.stringValue)
   for required in ["対象","理由","内容","確認済み","戻し方","承認  ⌘1","1日許可  ⌘2","却下  ⌘3"] { precondition(names.contains(required),"missing \(required)") }
   let hints=names.filter {$0.contains("⌘") && $0.contains("送信") && $0.contains("選ん")}
   precondition(hints.count == 1 && hints[0].contains("⌘1〜⌘3"),"guard instruction must be one line")
   print("ORDER title>command>request optionShortcuts=3 requestFields=5")
  }
  for card in controller.cards {precondition(content.bounds.contains(card.convert(card.bounds,to:content)))}
  precondition(content.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:content)))
  print("WINDOW case=\(name) number=\(controller.panel.windowNumber) visible=\(controller.panel.isVisible) frame=\(controller.panel.frame)")
  let file=root+"/"+name+"-"+theme.rawValue+".png"
  // Capture this actual AppKit window's content directly. This needs no
  // permission to read other windows or the user's desktop.
  let bitmap=content.bitmapImageRepForCachingDisplay(in:content.bounds)!
  content.cacheDisplay(in:content.bounds,to:bitmap)
  try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:file))
  // Count the same red pixels in a known RGBA buffer. Per-pixel NSColor
  // allocation made the expanded landscape test exceed its runtime bound.
  let width=bitmap.pixelsWide,height=bitmap.pixelsHigh
  var pixels=[UInt8](repeating:0,count:width*height*4)
  let redPixels=pixels.withUnsafeMutableBytes { bytes -> Int in
   let context=CGContext(data:bytes.baseAddress,width:width,height:height,bitsPerComponent:8,bytesPerRow:width*4,space:CGColorSpaceCreateDeviceRGB(),bitmapInfo:CGImageAlphaInfo.premultipliedLast.rawValue | CGBitmapInfo.byteOrder32Big.rawValue)!
   context.draw(bitmap.cgImage!,in:CGRect(x:0,y:0,width:width,height:height))
   var count=0
   for offset in stride(from:0,to:bytes.count,by:4) {if bytes[offset]>bytes[offset+1] && bytes[offset]>bytes[offset+2] {count+=1}}
   return count
  }
  if expected != nil { precondition(redPixels > 0,"expected visible red pixels for \(name)") }
  print("PASS case=\(name) theme=\(theme.rawValue) commandRanges=\(expected == nil ? 0 : 1) exactRedUTF16=\(redCharacters) redPixels=\(redPixels) selectable=1 links=preserved")
  controller.close()
 }
}
