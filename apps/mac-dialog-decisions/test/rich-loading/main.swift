import AppKit
import Foundation
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let root = CommandLine.arguments[1]
let image = NSImage(systemSymbolName: "checkmark", accessibilityDescription: nil)!
let data = NSBitmapImageRep(data: image.tiffRepresentation!)!.representation(using: .png, properties: [:])!
for textOnly in [false, true] {
  let specData = try JSONSerialization.data(withJSONObject: ["window_title":"画像取り込み試験","title":"画像取り込み中", "options":[["id":"yes","label":"選ぶ"]], "allow_text":true, "multi_select":false, "draft_directory":root])
  let controller = DialogController(spec:try JSONDecoder().decode(DialogSpec.self,from:specData),specPath:root+"/spec.json")
  controller.build()
  if textOnly { controller.textView!.string = "画像も届ける" } else { controller.cardClicked(controller.cards[0]) }
  controller.imageWork.isSuspended = true
  controller.stageAttachment(data:data,name:"日本語 空白.png")
  precondition(controller.attachmentLoading == 1)
  let enter = NSEvent.keyEvent(with:.keyDown,location:.zero,modifierFlags:[.command],timestamp:0,windowNumber:controller.panel.windowNumber,context:nil,characters:"\r",charactersIgnoringModifiers:"\r",isARepeat:false,keyCode:36)!
  precondition(controller.handleKey(enter) == nil)
  precondition(!controller.answerPending,"Command-Enter must not send while the real image import is queued")
  controller.imageWork.isSuspended = false
  let deadline = Date(timeIntervalSinceNow:5)
  while controller.attachmentLoading > 0 && Date() < deadline { RunLoop.current.run(until:Date(timeIntervalSinceNow:0.01)) }
  precondition(controller.attachments.count == 1 && controller.attachmentLoading == 0)
  precondition(controller.handleKey(enter) == nil)
  precondition(controller.answerPending,"completed attachment can be sent")
  controller.close()
}
print("IMPORT_SEND_PASS")
