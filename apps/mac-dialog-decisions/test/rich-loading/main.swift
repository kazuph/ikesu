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
let fifo = root + "/fifo-\(UUID().uuidString).png"
precondition(mkfifo(fifo, S_IRUSR | S_IWUSR) == 0)
defer { try? FileManager.default.removeItem(atPath:fifo) }
let fifoSpecData = try JSONSerialization.data(withJSONObject:["window_title":"FIFO試験","title":"通常ファイルのみ","options":[],"allow_text":true,"multi_select":false,"draft_directory":root])
let fifoController = DialogController(spec:try JSONDecoder().decode(DialogSpec.self,from:fifoSpecData),specPath:root+"/fifo-spec.json")
fifoController.build()
fifoController.attachFiles([URL(fileURLWithPath:fifo)])
let fifoDeadline = Date(timeIntervalSinceNow:5)
while fifoController.attachmentLoading > 0 && Date() < fifoDeadline {RunLoop.current.run(until:Date(timeIntervalSinceNow:0.01))}
precondition(fifoController.attachmentLoading == 0 && fifoController.attachments.isEmpty,"unwritten FIFO attachment must reject without blocking")
fifoController.loadRichImage(fifo,into:fifoController.richStack)
let previewDeadline = Date(timeIntervalSinceNow:5)
while fifoController.imageWork.operationCount > 0 && Date() < previewDeadline {RunLoop.current.run(until:Date(timeIntervalSinceNow:0.01))}
precondition(fifoController.imageWork.operationCount == 0,"unwritten FIFO preview must finish")
fifoController.stageAttachment(data:data,name:"通常画像.png")
let normalDeadline = Date(timeIntervalSinceNow:5)
while fifoController.attachmentLoading > 0 && Date() < normalDeadline {RunLoop.current.run(until:Date(timeIntervalSinceNow:0.01))}
precondition(fifoController.attachments.count == 1,"normal image must still work after FIFO rejection")
fifoController.close()
precondition(fifoController.imageWork.operations.allSatisfy{$0.isFinished || $0.isCancelled})
print("NATIVE_FIFO_PASS")
