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
precondition(richLinkRanges("https://example.com/a").count == 1)
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
controller.panel.contentView!.layoutSubtreeIfNeeded()
let view=controller.panel.contentView!
precondition(view.bounds.contains(controller.submitButton.convert(controller.submitButton.bounds,to:view)),"submit remains inside window")
precondition(controller.checkedIds().isEmpty,"adding images must not select")
precondition(!controller.answerPending,"adding images must not submit")
print("NATIVE_RICH_PASS attachments=2 namedPasteboard=1 buttonsInside=1 selected=0 sent=0")
controller.close()
