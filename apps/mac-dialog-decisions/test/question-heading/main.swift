import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let rows=try JSONSerialization.jsonObject(with:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1]))) as! [[String:Any]]
let root=CommandLine.arguments[2]
func labels(_ view:NSView)->[RichTextLabel] {view.subviews.flatMap {($0 as? RichTextLabel).map {[$0]} ?? labels($0)}}
var metrics:[[String:Any]]=[]
var failures:[String]=[]
for row in rows {
 let name=row["name"] as! String,body=row["body"] as! String,heading=row["heading"] as! String
 let spec=try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:row["spec"]!))
 for theme in [NSAppearance.Name.aqua,.darkAqua] {
  app.appearance=NSAppearance(named:theme)
  let controller=DialogController(spec:spec,specPath:root+"/"+name+".json")
  controller.build();controller.panel.appearance=app.appearance;controller.panel.orderBack(nil);controller.panel.display()
  let content=controller.panel.contentView!;content.layoutSubtreeIfNeeded()
  let host=DialogHost(directory:root);host.windows[name]=controller
  let menu=host.applicationDockMenu(app)!
  precondition(menu.items.count==1 && menu.items[0].title.hasSuffix(heading.isEmpty ? body:heading),"Dock menu keeps question identity")
  host.windows.removeAll()
  let rendered=labels(content)
  let count=rendered.filter {$0.string==body}.count
  let titles=rendered.filter {($0.attributedString().attribute(.font,at:0,effectiveRange:nil) as? NSFont)?.pointSize==Look.title}
  var partCounts:[String:Int]=[:]
  let text=rendered.map {$0.string}.joined(separator:"\n")
  for part in (row["parts"] as? [String] ?? [body]) {
    let copies=text.components(separatedBy:part).count-1;partCounts[part]=copies
    if copies != 1 {failures.append("\(name)/\(theme.rawValue): component \(part) copies=\(copies)")}
  }
  let wanted=heading.isEmpty ? 0:1
  if count != 1 || titles.count != wanted || (!heading.isEmpty && titles.first?.string != heading) {failures.append("\(name)/\(theme.rawValue): body copies=\(count), headings=\(titles.map {$0.string})")}
  for card in controller.cards {precondition(content.bounds.contains(card.convert(card.bounds,to:content)))}
  precondition(content.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:content)))
  content.wantsLayer=true
  controller.panel.effectiveAppearance.performAsCurrentDrawingAppearance {content.layer!.backgroundColor=NSColor.windowBackgroundColor.cgColor}
  let bitmap=content.bitmapImageRepForCachingDisplay(in:content.bounds)!
  content.cacheDisplay(in:content.bounds,to:bitmap)
  let file=name+"-"+theme.rawValue+".png"
  try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:root+"/"+file))
  metrics.append(["case":name,"theme":theme.rawValue,"questionCopies":count,"headings":titles.map {$0.string},"image":file,"window":controller.panel.windowNumber,"choicesVisible":true,"partCounts":partCounts])
  controller.close()
 }
}
try JSONSerialization.data(withJSONObject:metrics,options:[.prettyPrinted,.sortedKeys]).write(to:URL(fileURLWithPath:root+"/metrics.json"))
for error in failures {print("FAIL \(error)")}
if !failures.isEmpty {exit(1)}
print("QUESTION_HEADING_NATIVE_PASS cases=\(metrics.count)")
