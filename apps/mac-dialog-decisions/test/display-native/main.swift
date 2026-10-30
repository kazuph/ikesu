import AppKit
import AVKit
import WebKit
import Foundation

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.finishLaunching()
let root = CommandLine.arguments[1]
func drain() async { try? await Task.sleep(nanoseconds:50_000_000) }
func visible(_ view: NSView, in root: NSView) -> Bool {
  if !root.bounds.contains(view.convert(view.bounds, to: root)) { return false }
  var parent = view.superview
  while let ancestor = parent {
    if let clip = ancestor as? NSClipView, !clip.bounds.contains(view.convert(view.bounds, to: clip)) { return false }
    parent = ancestor.superview
  }
  return true
}
func labels(_ view: NSView) -> [RichTextLabel] {
  view.subviews.flatMap { child in (child as? RichTextLabel).map {[$0]} ?? labels(child) }
}
@MainActor func run() async throws {
var rows: [[String: Any]] = []
for theme in [NSAppearance.Name.aqua, .darkAqua] {
 app.appearance = NSAppearance(named: theme)
 for kind in ["command_guard", "agent_prompt"] {
  let source = root + "/sample.mp4"
  let input: [String: Any] = ["kind":kind,"window_title":"判断待ちの表示試験", "location":"ローカル｜ikesu｜開発｜実装 / codex", "approval_command":kind == "command_guard" ? "rm /tmp/example" : "", "title":"削除操作の確認・質問見出しの全文", "body":String(repeating:"主要な説明と細かい該当理由です。\n",count:200) + source, "options":[["id":"approve","label":"承認","description":"この実行を許可"],["id":"day","label":"1日許可"],["id":"reject","label":"却下"]],"allow_text":true,"multi_select":false,"media_sources":[source,"https://example.com"],"draft_directory":root]
  let file = root + "/" + kind + ".json"
  let data = try JSONSerialization.data(withJSONObject:input)
  try data.write(to:URL(fileURLWithPath:file))
  let controller=DialogController(spec:try JSONDecoder().decode(DialogSpec.self,from:data),specPath:file)
  controller.build();controller.panel.appearance=app.appearance;controller.panel.orderBack(nil);await drain()
  controller.respond(["status":"rich_video","source":source,"path":source])
  let content=controller.panel.contentView!
  content.layoutSubtreeIfNeeded();await drain()
  precondition(content.bounds.width > content.bounds.height,"default must be landscape")
  precondition(controller.players.count==1 && controller.players[0].isMuted && controller.players[0].rate==0,"video starts muted and paused")
  precondition(controller.richScroll.superview === content,"media is outside body scroll")
  for card in controller.cards {precondition(visible(card,in:content),"all choices visible")}
  precondition(visible(controller.submitButton,in:content),"submit visible")
  let card=controller.mediaCards[source]!
  precondition(card.convert(card.bounds,to:content).minX > content.bounds.width/2,"media in right column")
  let body=labels(content).first {$0.string.hasPrefix("主要な説明")}!
  precondition(body.enclosingScrollView !== controller.richScroll,"body scroll independent")
  let location = content.subviews.flatMap {labels($0)}.first {$0.string == input["location"] as? String}
  // Location uses a wrapping NSTextField, so check all fields independently.
  func findLocation(_ view:NSView)->NSTextField? {
   if let field=view as? NSTextField,field.stringValue==input["location"] as? String {return field}
   for child in view.subviews {if let found=findLocation(child){return found}};return nil
  }
  precondition(findLocation(content) != nil || location != nil)
  let title=labels(content).first {$0.string==input["title"] as? String}!
  if kind=="command_guard" {
   let command=labels(content).first {$0.string=="rm /tmp/example"}!
   precondition(command.convert(command.bounds,to:content).minY > title.convert(title.bounds,to:content).minY,"command precedes reason")
   for index in 0..<(command.string as NSString).length {
    precondition(command.attributedString().attribute(.foregroundColor,at:index,effectiveRange:nil) as? NSColor == .systemRed)
    precondition((command.attributedString().attribute(.font,at:index,effectiveRange:nil) as! NSFont).isFixedPitch)
   }
  }
  controller.cardClicked(controller.cards[0]);precondition(controller.checkedIds()==["approve"])
  controller.textView!.insertText("コメントだけ",replacementRange:NSRange(location:NSNotFound,length:0))
  precondition(controller.checkedIds().isEmpty,"typing must clear selection")
  controller.cardClicked(controller.cards[2]);precondition(controller.checkedIds()==["reject"] && controller.currentText()=="コメントだけ","explicit reselection includes comment")
  controller.textView!.selectAll(nil);controller.textView!.insertText("",replacementRange:NSRange(location:0,length:(controller.textView!.string as NSString).length))
  precondition(controller.checkedIds()==["reject"],"clearing comment must not alter explicit later choice")
  controller.textView!.insertText("新しいコメント",replacementRange:NSRange(location:NSNotFound,length:0));precondition(controller.checkedIds().isEmpty)
  controller.textView!.string="";controller.textDidChange(Notification(name:NSText.didChangeNotification));precondition(controller.checkedIds().isEmpty,"deleting comment must not restore old selection")
  content.wantsLayer=true
  controller.panel.effectiveAppearance.performAsCurrentDrawingAppearance {content.layer!.backgroundColor=NSColor.windowBackgroundColor.cgColor}
  let image=content.bitmapImageRepForCachingDisplay(in:content.bounds)!
  content.cacheDisplay(in:content.bounds,to:image)
  let imagePath=root+"/"+kind+"-"+theme.rawValue+".png"
  try image.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:imagePath))
  let normal=[content.bounds.width,content.bounds.height]
  controller.panel.setContentSize(NSSize(width:controller.panel.minSize.width,height:controller.panel.minSize.height-(controller.panel.frame.height-controller.panel.contentLayoutRect.height)))
  content.layoutSubtreeIfNeeded()
  for choice in controller.cards {precondition(visible(choice,in:content),"choice visible at minimum size")}
  precondition(visible(controller.submitButton,in:content),"submit visible at minimum size")
  rows.append(["kind":kind,"theme":theme.rawValue,"size":normal,"choices":controller.cards.count,"image":imagePath,"muted":true,"rate":0,"commentClearsSelection":true])
  controller.close()
 }
}
// Decode and advance actual video data silently through each native player.
for ext in ["mp4", "mov", "m4v", "webm"] {
 let file=root+"/sample."+ext
 let input:[String:Any] = ["window_title":"動画試験","title":"動画を右で再生","options":[],"allow_text":false,"multi_select":false,"media_sources":[file]]
 let controller=DialogController(spec:try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:input)),specPath:root+"/video-spec.json")
 controller.build();controller.panel.orderBack(nil)
 controller.respond(["status":"rich_video","source":file,"path":file])
 let end=Date(timeIntervalSinceNow:5)
 if let web=controller.webVideos.first {
  func evaluate(_ script:String) async throws -> Any? {
   try await withCheckedThrowingContinuation { continuation in
    web.evaluateJavaScript(script) {value,error in
     if let error {continuation.resume(throwing:error)} else {continuation.resume(returning:value)}
    }
   }
  }
  var ready=false
  while Date()<end {
   if let state=try await evaluate("(()=>{let v=document.querySelector('video');return v ? {width:v.videoWidth,muted:v.muted,paused:v.paused,error:v.error?.message||''}:null})()") as? [String:Any], (state["width"] as? Int ?? 0)>0 {
    precondition(state["muted"] as? Bool == true && state["paused"] as? Bool == true);ready=true;break
   };await drain()
  }
  precondition(ready,"actual WebM must decode a frame")
  _=try await evaluate("void document.querySelector('video').play()")
  var advanced=false
  while Date()<end {if (try await evaluate("document.querySelector('video').currentTime") as? Double ?? 0)>0 {advanced=true;break};await drain()}
  precondition(advanced,"WebM must play silently after explicit play")
  _=try await evaluate("document.querySelector('video').pause(); document.querySelector('video').paused")
 } else {
  let player=controller.players.first!
  precondition(player.isMuted && player.rate==0)
  player.play()
  while CMTimeGetSeconds(player.currentTime())<=0 && Date()<end {await drain()}
  precondition(CMTimeGetSeconds(player.currentTime())>0,"actual \(ext) must advance after explicit play")
  player.pause();precondition(player.rate==0)
 }
 controller.close();print("VIDEO_PLAY_PAUSE_PASS format=\(ext) initialMuted=1 initialPaused=1 decoded=1 advanced=1")
}
// Exercise NSTextView's actual link-click path for every occurrence, including
// percent-encoded Japanese/space/percent filenames, without opening a browser.
let source=root+"/日本語 空白%clip.mp4"
let encoded=URL(fileURLWithPath:source).path(percentEncoded:true)
let linkBody="[動画](<"+encoded+">)\n同じリンク: "+encoded+"\n通常パス: "+source
let preceding=(0..<7).map {"https://example.com/"+String(repeating:"long-path-",count:15)+"/\($0)"}
let linkInput:[String:Any] = ["window_title":"ローカルリンク試験","title":"本文から右のカードへ移動","body":linkBody,"options":[],"allow_text":false,"multi_select":false,"media_sources":preceding+[source]]
let links=DialogController(spec:try JSONDecoder().decode(DialogSpec.self,from:JSONSerialization.data(withJSONObject:linkInput)),specPath:root+"/links.json")
var openedLinks:[URL]=[]
links.openLink={openedLinks.append($0);return true}
links.build();links.panel.orderBack(nil);links.panel.setContentSize(NSSize(width:1080,height:350));await drain()
let text=labels(links.panel.contentView!).first {$0.string==linkBody}!
let value=text.string as NSString
var clicks=0
for candidate in [encoded,source] {
 var offset=0
 while offset<value.length {
  let range=value.range(of:candidate,options:[],range:NSRange(location:offset,length:value.length-offset))
  if range.location==NSNotFound {break}
  let attribute=text.attributedString().attribute(.link,at:range.location,effectiveRange:nil) as! String
  precondition(attribute==source,"every link maps to the original unencoded source")
  links.richScroll.contentView.scroll(to:.zero);links.richScroll.reflectScrolledClipView(links.richScroll.contentView)
  text.clicked(onLink:attribute,at:range.location)
  precondition(openedLinks.last==URL(fileURLWithPath:source) && openedLinks.count==clicks+1,"native click must open the corresponding file")
  precondition(text.string==linkBody,"click must not rewrite visible text")
  clicks+=1;offset=NSMaxRange(range)
 }
}
precondition(clicks==3,"both encoded occurrences and the literal path must work")
links.close();print("LOCAL_MEDIA_LINKS_PASS actualTextViewClicks=\(clicks) encodedJapaneseSpacePercent=1 textPreserved=1 fileOpened=1")
// One native host must preserve window order and keyboard focus for new requests.
let host=DialogHost(directory:root)
app.delegate=host
let before=NSWorkspace.shared.frontmostApplication?.processIdentifier
for index in 1...3 {
 let file=root+"/stack-\(index).json"
 let input:[String:Any] = ["window_title":"順序 \(index)","title":"判断待ち \(index)","options":[["id":"yes","label":"はい"]],"allow_text":false,"multi_select":false]
 try JSONSerialization.data(withJSONObject:input).write(to:URL(fileURLWithPath:file))
 let previous=app.keyWindow
 host.open(file);await drain()
 let numbers=NSWindow.windowNumbers(options:[.allSpaces]) ?? []
 let ordered=numbers.compactMap {number in host.windows.values.first {$0.panel.windowNumber==number.intValue}?.panel}
 print("ORDER numbers=\(numbers) own=\(ordered.map{$0.windowNumber})")
 precondition(ordered.count==index)
 precondition(ordered.last===host.windows["stack-\(index).json"]!.panel,"new panel must be behind existing panels")
 precondition(app.keyWindow===previous,"new panel cannot steal key window")
 precondition(NSWorkspace.shared.frontmostApplication?.processIdentifier==before,"new panel cannot activate app")
}
host.closeAll()
try JSONSerialization.data(withJSONObject:rows,options:[.prettyPrinted,.sortedKeys]).write(to:URL(fileURLWithPath:root+"/native-metrics.json"))
print("DISPLAY_NATIVE_PASS cases=4 choicesAlwaysVisible=1 mediaRight=1 videoMutedPaused=1 commentTransitions=1 newWindowsBehind=3 focusPreserved=1")

}
Task { @MainActor in do { try await run(); exit(0) } catch { fputs("\(error)\n",stderr); exit(1) } }
app.run()
