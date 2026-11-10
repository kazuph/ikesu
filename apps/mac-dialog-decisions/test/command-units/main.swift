import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let spec=try JSONDecoder().decode(DialogSpec.self,from:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1])))
let out=CommandLine.arguments[2]
func labels(_ view:NSView)->[NSTextField] {view.subviews.flatMap {($0 as? NSTextField).map {[$0]} ?? labels($0)}}
for theme in [NSAppearance.Name.aqua,.darkAqua] {
 app.appearance=NSAppearance(named:theme)
 let controller=DialogController(spec:spec,specPath:out+"/spec.json")
 controller.build();controller.panel.appearance=app.appearance;controller.panel.orderBack(nil);controller.panel.display()
 RunLoop.current.run(until:Date(timeIntervalSinceNow:0.1))
 let content=controller.panel.contentView!
 content.wantsLayer=true
 controller.panel.effectiveAppearance.performAsCurrentDrawingAppearance {content.layer!.backgroundColor=NSColor.windowBackgroundColor.cgColor}
 content.layoutSubtreeIfNeeded()
 let all=labels(content)
 let strings=all.map(\.stringValue)
 for unit in spec.approval_units ?? [] { precondition(strings.contains(unit.text),"missing unit text \(unit.text)") }
 precondition(strings.contains(spec.approval_units_heading ?? ""),"heading missing")
 precondition(!strings.contains(spec.approval_command ?? "<none>"),"whole command must not be shown when units exist")
 precondition(!labels(content).isEmpty)
 let asks=(spec.approval_units ?? []).filter {$0.verdict=="ask"||$0.verdict=="block"}
 precondition(strings.filter {$0.hasPrefix("理由: ")}.count==asks.filter {(!($0.reason ?? "").isEmpty) || (!($0.rule ?? "").isEmpty)}.count,"reason lines")
 precondition(strings.filter {$0=="確認なし"}.count==(spec.approval_units ?? []).filter {$0.verdict=="allow"}.count,"allow notes")
 let bitmap=content.bitmapImageRepForCachingDisplay(in:content.bounds)!
 content.cacheDisplay(in:content.bounds,to:bitmap)
 try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:out+"/units-"+theme.rawValue+".png"))
}
print("PASS units=\(spec.approval_units?.count ?? 0)")
