import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
app.appearance = NSAppearance(named: .darkAqua)
let root = CommandLine.arguments[1]
let specData = try JSONSerialization.data(withJSONObject: ["window_title":"rich test", "title":"公開リンク https://example.com/a", "body":String(repeating:"長い本文です。",count:500), "options":[["id":"yes","label":"選ぶ","description":"https://example.com/b"]], "allow_text":true,"multi_select":false,"draft_directory":root])
let spec = try JSONDecoder().decode(DialogSpec.self,from:specData)
let controller = DialogController(spec:spec,specPath:root+"/spec.json")
controller.build()
controller.panel.orderFront(nil)
precondition(richLinkRanges("https://example.com/a").count == 1)
precondition(richLinkRanges("/Users/kazuph/src/github.com/kazuph/image.png").isEmpty, "file paths are not http links")
let board = NSPasteboard(name:NSPasteboard.Name("dialog-rich-test-\(UUID().uuidString)"))
board.clearContents()
let image = NSImage(systemSymbolName:"checkmark",accessibilityDescription:nil)!
let bitmap = NSBitmapImageRep(data:image.tiffRepresentation!)!
let data = bitmap.representation(using:.png,properties:[:])!
board.setData(data,forType:.png)
precondition(controller.consumeAttachments(board),"named pasteboard must attach an image")
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.5))
precondition(controller.attachments.count==1,"paste image must remain in draft")
precondition(controller.submitButton.isEnabled,"image-only answer must be enabled")
let source = root+"/日本語 空白.png"
try data.write(to:URL(fileURLWithPath:source))
controller.attachFiles([URL(fileURLWithPath:source)])
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.5))
precondition(controller.attachments.count==2)
let tiffBoard = NSPasteboard(name:NSPasteboard.Name("dialog-rich-tiff-\(UUID().uuidString)"))
tiffBoard.clearContents();tiffBoard.setData(image.tiffRepresentation!,forType:.tiff)
controller.textView!.imagePasteboard = tiffBoard
controller.panel.makeKey();controller.panel.makeFirstResponder(controller.textView!)
let commandV = NSEvent.keyEvent(with:.keyDown,location:.zero,modifierFlags:[.command],timestamp:0,windowNumber:controller.panel.windowNumber,context:nil,characters:"v",charactersIgnoringModifiers:"v",isARepeat:false,keyCode:9)!
precondition(controller.handleKey(commandV) == nil,"Command-V must route through the native edit action")
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.5))
precondition(controller.attachments.count==3,"Command-V uses the named TIFF pasteboard")
precondition(controller.attachments.last!.path.hasSuffix(".png"),"clipboard bitmap is serialized as PNG")
for broken in [Data(),Data("broken image".utf8),Data(count:10*1024*1024+1)] {
 do { _ = try richBitmap(broken);fatalError("invalid or oversized image was accepted") } catch {}
}
let wide=NSBitmapImageRep(bitmapDataPlanes:nil,pixelsWide:8001,pixelsHigh:1,bitsPerSample:8,samplesPerPixel:4,hasAlpha:true,isPlanar:false,colorSpaceName:.deviceRGB,bytesPerRow:0,bitsPerPixel:0)!
do { _ = try richBitmap(wide.representation(using:.png,properties:[:])!);fatalError("8001 pixel image was accepted") } catch {}
controller.panel.contentView!.layoutSubtreeIfNeeded()
let view=controller.panel.contentView!
precondition(view.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:view)),"submit remains inside window")
precondition(controller.checkedIds().isEmpty,"adding images must not select")
precondition(!controller.answerPending,"adding images must not submit")
controller.respond(["status":"rich_card", "title":"公開ページの題名", "url":"https://example.com/a", "site":"example.com", "path":source])
controller.respond(["status":"rich_image", "path":source])
RunLoop.current.run(until:Date(timeIntervalSinceNow:0.5))
view.layoutSubtreeIfNeeded()
for width in [720.0, 520.0] {
 controller.panel.setContentSize(NSSize(width:width,height:controller.panel.contentLayoutRect.height));view.layoutSubtreeIfNeeded()
 precondition(view.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:view)),"submit visible with cards and images at width \(width)")
 for button in controller.attachmentRow.arrangedSubviews { precondition(view.bounds.contains(button.convert(button.bounds,to:view)),"attachments remain inside window") }
}
controller.panel.setContentSize(NSSize(width:720,height:controller.panel.contentLayoutRect.height));view.layoutSubtreeIfNeeded()
let capture=Process();capture.executableURL=URL(fileURLWithPath:"/usr/sbin/screencapture");capture.arguments=["-x","-l",String(controller.panel.windowNumber),root+"/rich-native.png"];try capture.run();capture.waitUntilExit();precondition(capture.terminationStatus==0)
print("NATIVE_RICH_PASS attachments=3 namedPasteboard=1 commandV=1 tiff=1 invalidRejected=1 buttonsInside=1 selected=0 sent=0")
controller.close()
