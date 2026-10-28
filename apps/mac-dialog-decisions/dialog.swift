// AppKit controller for one decision window; DialogHost owns process lifetime.
// New windows appear once without taking focus. Selection requires explicit submit.
// Draft, result, shown and closed events carry the window key.

import AppKit
import Foundation
import ImageIO
import UniformTypeIdentifiers

// MARK: - spec

struct DialogOption: Decodable {
  let id: String
  let label: String
  let description: String?
  let preview: String?
  let preview_unavailable: Bool?
}

struct DialogDetail: Decodable {
  let label: String
  let value: String
  let monospace: Bool?
}

struct DialogSpec: Decodable {
  let present_in_background: Bool?
  let kind: String?
  let window_title: String
  let source: String?        // 🤖 claude
  let location: String?      // プロジェクト › タブ › ペイン
  let machine: String?       // shown only for a saved machine
  let question_label: String?
  let title: String
  let body: String?
  let details: [DialogDetail]?
  let options: [DialogOption]
  let multi_select: Bool
  let allow_text: Bool
  let deadline: String?
  let queue_remaining: Int?
  let later_label: String?
  let result_path: String?
  let daemon_pid: Int32?
  let daemon_owner: String?
  let daemon_ownership_path: String?
  let draft: DialogDraft?
  let draft_directory: String?
}

struct DialogAttachment: Codable {
  let path: String
  let name: String
  var object: [String: String] { ["path": path, "name": name] }
}
struct DialogDraft: Decodable { let option_ids: [String]; let text: String; let attachments: [DialogAttachment]? }

// MARK: - output

func writeOutput(_ object: [String: Any], resultPath: String? = nil) {
  guard let data = try? JSONSerialization.data(withJSONObject: object),
    var line = String(data: data, encoding: .utf8)
  else { return }
  line += "\n"
  FileHandle.standardOutput.write(line.data(using: .utf8)!)
  if let path = resultPath {
    if let handle = FileHandle(forWritingAtPath: path) {
      handle.seekToEndOfFile()
      handle.write(line.data(using: .utf8)!)
      handle.closeFile()
    } else {
      FileManager.default.createFile(atPath: path, contents: line.data(using: .utf8))
    }
  }
}

// MARK: - look

enum Look {
  static let base: CGFloat = 15
  static let small: CGFloat = 13
  static let title: CGFloat = 19
  static let width: CGFloat = 720
  static let pad: CGFloat = 22
  static let accent = NSColor.controlAccentColor
}

// Match the guard's field-boundary formatting when comparing the original
// request with its display copy; keep whitespace inside each field intact.
func comparableApprovalDetail(_ text: String) -> String {
  text.replacingOccurrences(of: #"[\t \r\n]*(対象プロジェクト|必要な理由|実施内容|確認済み|戻し方)[:：]"#,
    with: "$1:", options: .regularExpression)
    .replacingOccurrences(of: "\n", with: "")
    .replacingOccurrences(of: "：", with: ":")
}

func label(_ text: String, size: CGFloat, weight: NSFont.Weight = .regular,
           color: NSColor = .labelColor, mono: Bool = false, selectable: Bool = true) -> NSTextField {
  let field = NSTextField(wrappingLabelWithString: text)
  field.font = mono
    ? NSFont.monospacedSystemFont(ofSize: size, weight: weight)
    : NSFont.systemFont(ofSize: size, weight: weight)
  field.textColor = color
  field.isSelectable = selectable
  field.lineBreakMode = .byWordWrapping
  field.translatesAutoresizingMaskIntoConstraints = false
  field.setContentCompressionResistancePriority(.required, for: .vertical)
  return field
}

// Accept the first click: the panel is usually not key when the user reaches
// for it, and AppKit otherwise swallows that click to activate the window.
final class FirstClickButton: NSButton {
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override var needsPanelToBecomeKey: Bool { true }
}

final class FirstClickImageView: NSImageView {
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override var needsPanelToBecomeKey: Bool { true }
}

final class FirstClickTextView: NSTextView {
  var imagePasteboard: NSPasteboard = .general
  var onAttachments: ((NSPasteboard) -> Bool)?
  override func paste(_ sender: Any?) {
    if onAttachments?(imagePasteboard) == true { return }; super.paste(sender)
  }
  override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
    if sender.draggingPasteboard.canReadObject(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) || sender.draggingPasteboard.availableType(from: [.png, .tiff]) != nil { return .copy }; return super.draggingEntered(sender)
  }
  override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
    if onAttachments?(sender.draggingPasteboard) == true { return true }; return super.performDragOperation(sender)
  }
  var onCommandReturn: (() -> Void)?
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override func keyDown(with event: NSEvent) {
    if event.modifierFlags.contains(.command),
      event.keyCode == 36 || event.keyCode == 76
    {
      onCommandReturn?()
      return
    }
    super.keyDown(with: event)
  }
}

// A non-activating panel becomes key (without activating the process) when
// the clicked view asks for it, so every view in the panel does: after any
// click inside, Esc / ⌘↩ / ⌘1… reach the panel.
//
// Showing a panel does not activate the process. Explicit window selection
// (click, Dock, app switcher or Mission Control) may activate it for input.
final class KeyPanel: NSPanel {
  override var canBecomeKey: Bool { true }
  override var canBecomeMain: Bool { true }
  override func sendEvent(_ event: NSEvent) {
    if event.type == .leftMouseDown,
      NSWorkspace.shared.frontmostApplication?.processIdentifier
        != ProcessInfo.processInfo.processIdentifier
    {
      NSApp.activate(ignoringOtherApps: true)
      makeKey()
    }
    super.sendEvent(event)
  }
  override func cancelOperation(_ sender: Any?) {
    (delegate as? DialogController)?.later()
  }
}

final class KeyRootView: NSView {
  override var needsPanelToBecomeKey: Bool { true }
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
}

final class FlippedView: NSView {
  override var needsPanelToBecomeKey: Bool { true }
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override var isFlipped: Bool { true }
}

// One option: number, label, description and preview in a clickable card.
final class OptionCard: NSView {
  let option: DialogOption
  let index: Int
  let multi: Bool
  var checked = false { didSet { refresh() } }
  var onClick: ((OptionCard) -> Void)?
  private var hovering = false { didSet { refresh() } }
  private var descriptionField: RichTextLabel?
  private let mark = NSTextField(labelWithString: "")
  private let box = FirstClickImageView()

  init(option: DialogOption, index: Int, multi: Bool) {
    self.option = option
    self.index = index
    self.multi = multi
    super.init(frame: .zero)
    translatesAutoresizingMaskIntoConstraints = false
    wantsLayer = true
    layer?.cornerRadius = 8
    layer?.borderWidth = 1

    mark.font = NSFont.systemFont(ofSize: Look.base + 1, weight: .semibold)
    mark.translatesAutoresizingMaskIntoConstraints = false
    mark.alignment = .center

    let stack = NSStackView()
    stack.orientation = .vertical
    stack.alignment = .leading
    stack.spacing = 4
    stack.translatesAutoresizingMaskIntoConstraints = false
    let title = label(option.label, size: Look.base + 1, weight: .semibold, selectable: false)
    stack.addArrangedSubview(title)
    if let d = option.description, !d.isEmpty {
      let field = richLabel(d, size: Look.small + 1, color: .secondaryLabelColor, selectable: false)
      descriptionField = field
      stack.addArrangedSubview(field)
    }
    if let p = option.preview, !p.isEmpty {
      let box = previewScroll(p)
      stack.addArrangedSubview(box)
      box.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
    } else if option.preview_unavailable == true {
      stack.addArrangedSubview(label("プレビューあり（エージェントの画面で確認）", size: Look.small, color: .tertiaryLabelColor, selectable: false))
    }
    addSubview(mark)
    addSubview(stack)
    box.translatesAutoresizingMaskIntoConstraints = false
    addSubview(box)
    NSLayoutConstraint.activate([
      box.centerXAnchor.constraint(equalTo: mark.centerXAnchor),
      box.topAnchor.constraint(equalTo: topAnchor, constant: 10),
      mark.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 12),
      mark.topAnchor.constraint(equalTo: topAnchor, constant: 11),
      mark.widthAnchor.constraint(equalToConstant: 34),
      stack.leadingAnchor.constraint(equalTo: mark.trailingAnchor, constant: 8),
      stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -14),
      stack.topAnchor.constraint(equalTo: topAnchor, constant: 11),
      stack.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -11),
    ])
    addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeAlways, .inVisibleRect], owner: self))
    toolTip = multi
      ? "クリックで選ぶ／外す（⌘\(index + 1)）"
      : "クリックで選ぶ（⌘\(index + 1)）。送るのは右下の「回答する」"
    refresh()
  }

  required init?(coder: NSCoder) { fatalError() }

  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override var needsPanelToBecomeKey: Bool { true }
  override func mouseEntered(with event: NSEvent) { hovering = true }
  override func mouseExited(with event: NSEvent) { hovering = false }
  override func mouseDown(with event: NSEvent) { layer?.opacity = 0.75 }
  override func mouseUp(with event: NSEvent) {
    layer?.opacity = 1
    // AppKit bubbles the field editor's mouse-up to the card after opening a
    // link. Test the actual glyph under that event, leaving other card clicks intact.
    if let editor = descriptionField,
      let manager = editor.layoutManager, let container = editor.textContainer, let storage = editor.textStorage {
      let local = editor.convert(event.locationInWindow, from: nil)
      let textPoint = NSPoint(x: local.x - editor.textContainerOrigin.x, y: local.y - editor.textContainerOrigin.y)
      let glyph = manager.glyphIndex(for: textPoint, in: container)
      if editor.bounds.contains(local), glyph < manager.numberOfGlyphs,
        manager.boundingRect(forGlyphRange: NSRange(location: glyph, length: 1), in: container).contains(textPoint) {
        let character = manager.characterIndexForGlyph(at: glyph)
        if character < storage.length, storage.attribute(.link, at: character, effectiveRange: nil) != nil { return }
      }
    }
    if bounds.contains(convert(event.locationInWindow, from: nil)) { onClick?(self) }
  }
  override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }

  private func refresh() {
    // Checkbox for multi-choice, radio button for one-choice. A click only
    // selects; nothing is sent until 回答する / ⌘↩.
    mark.stringValue = ""
    let name = multi
      ? (checked ? "checkmark.square.fill" : "square")
      : (checked ? "largecircle.fill.circle" : "circle")
    let config = NSImage.SymbolConfiguration(pointSize: Look.base + 5, weight: .regular)
    box.image = NSImage(systemSymbolName: name, accessibilityDescription: checked ? "選択中" : "未選択")?
      .withSymbolConfiguration(config)
    box.contentTintColor = checked ? Look.accent : .secondaryLabelColor
    let active = checked || hovering
    mark.textColor = active ? Look.accent : .secondaryLabelColor
    // Layer colors are plain CGColors: resolve the dynamic system colors in
    // this view's own appearance (light or dark), and again whenever it
    // changes — otherwise the cards keep whichever look was current at
    // creation and become unreadable in the other mode.
    effectiveAppearance.performAsCurrentDrawingAppearance {
      layer?.borderColor = (checked ? Look.accent : (hovering ? Look.accent.withAlphaComponent(0.6) : NSColor.separatorColor)).cgColor
      layer?.backgroundColor = (active ? Look.accent.withAlphaComponent(checked ? 0.18 : 0.08) : NSColor.controlBackgroundColor).cgColor
    }
  }

  override func viewDidChangeEffectiveAppearance() {
    super.viewDidChangeEffectiveAppearance()
    refresh()
  }
}

// MARK: - controller

final class DialogController: NSObject, NSWindowDelegate {
  let spec: DialogSpec
  let specPath: String
  var panel: KeyPanel!
  var cards: [OptionCard] = []
  var textView: FirstClickTextView?
  var submitButton: FirstClickButton!
  var attachments: [DialogAttachment] = []
  var attachmentLoading = 0
  let attachmentRow = NSStackView()
  let richStack = NSStackView()
  var richWarningShown = false
  let imageWork = OperationQueue()
  var answerPending = false
  let statusLabel = NSTextField(wrappingLabelWithString: "")
  var infoLabel: NSTextField!
  var onClose: ((DialogController) -> Void)?
  var closed = false
  var key: String { URL(fileURLWithPath: specPath).lastPathComponent }

  init(spec: DialogSpec, specPath: String) {
    self.spec = spec
    self.specPath = specPath
  }

  func checkedIds() -> [String] { cards.filter { $0.checked }.map { $0.option.id } }
  func currentText() -> String {
    (textView?.string ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
  }

  func emit(_ object: [String: Any]) {
    var event = object
    event["key"] = key
    writeOutput(event, resultPath: spec.result_path)
  }

  func saveDraft() {
    emit(["event": "draft", "draft": ["option_ids": checkedIds(), "text": textView?.string ?? "", "attachments": attachments.map { $0.object }]])
  }

  func close() {
    if closed { return }
    closed = true
    imageWork.cancelAllOperations()
    saveDraft()
    panel.orderOut(nil)
    panel.close()
    emit(["event": "closed"])
    onClose?(self)
  }

  func answer(ids: [String]) {
    guard !answerPending, !closed, attachmentLoading == 0 else { return }
    answerPending = true
    statusLabel.stringValue = "回答を送信中…"
    refreshSubmit()
    emit(["result": "answer", "option_ids": ids, "text": currentText(), "attachments": attachments.map { $0.object }])
  }

  @objc func submit() {
    let ids = checkedIds()
    let text = currentText()
    if ids.isEmpty && text.isEmpty && attachments.isEmpty {
      statusLabel.stringValue = "選択肢を選ぶか、回答を入力してください。"
      return
    }
    answer(ids: ids)
  }

  @objc func later() { emit(["result": "later"]); close() }

  func windowShouldClose(_ sender: NSWindow) -> Bool {
    later()
    return false
  }

  func cardClicked(_ card: OptionCard) {
    // Selecting never sends: a panel that appears under the pointer while the
    // user is busy elsewhere must not turn a stray click into an answer.
    if spec.multi_select {
      card.checked.toggle()
    } else {
      // Clicking the chosen option again clears it, so a text-only answer
      // stays possible after a choice.
      let clear = card.checked
      for other in cards { other.checked = !clear && other === card }
    }
    refreshSubmit()
    saveDraft()
  }

  func refreshSubmit() {
    let ready = !checkedIds().isEmpty || !currentText().isEmpty || !attachments.isEmpty
    submitButton.isEnabled = ready && !answerPending && attachmentLoading == 0
  }

  func handleKey(_ event: NSEvent) -> NSEvent? {
    guard event.window === panel else { return event }
    let cmd = event.modifierFlags.contains(.command)
    if event.keyCode == 53 { later(); return nil }                 // Esc
    if cmd && (event.keyCode == 36 || event.keyCode == 76) {        // ⌘↩
      submit(); return nil
    }
    if cmd, let ch = event.charactersIgnoringModifiers, let n = Int(ch), n >= 1, n <= cards.count {
      cardClicked(cards[n - 1]); return nil
    }
    // The dialog has no menu bar, and ⌘V / ⌘C / ⌘X / ⌘A / ⌘Z reach a text
    // field only through the Edit menu's key equivalents. Send the same
    // standard edit actions to the focused field directly.
    if cmd, let action = editAction(event) {
      if NSApp.sendAction(action, to: panel.firstResponder, from: panel) { return nil }
    }
    return event
  }

  func editAction(_ event: NSEvent) -> Selector? {
    let mods = event.modifierFlags.intersection([.command, .shift, .option, .control])
    switch (event.charactersIgnoringModifiers?.lowercased(), mods) {
    case ("v", [.command]): return #selector(NSText.paste(_:))
    case ("c", [.command]): return #selector(NSText.copy(_:))
    case ("x", [.command]): return #selector(NSText.cut(_:))
    case ("a", [.command]): return #selector(NSText.selectAll(_:))
    case ("z", [.command]): return Selector(("undo:"))
    case ("z", [.command, .shift]): return Selector(("redo:"))
    default: return nil
    }
  }

  func build() {
    let content = FlippedView()
    content.translatesAutoresizingMaskIntoConstraints = false

    let stack = NSStackView()
    stack.orientation = .vertical
    stack.alignment = .leading
    stack.spacing = 12
    stack.translatesAutoresizingMaskIntoConstraints = false
    content.addSubview(stack)
    NSLayoutConstraint.activate([
      stack.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: Look.pad),
      stack.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -Look.pad),
      stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 18),
      stack.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -14),
    ])
    func full(_ v: NSView) {
      stack.addArrangedSubview(v)
      v.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
    }

    // Where it comes from — the first thing the eye lands on.
    var whereParts: [String] = []
    if let s = spec.source, !s.isEmpty { whereParts.append(s) }
    if let m = spec.machine, !m.isEmpty { whereParts.append("🖥 \(m)") }
    if !whereParts.isEmpty {
      full(label(whereParts.joined(separator: "   "), size: Look.small + 1, weight: .medium, color: .secondaryLabelColor))
    }
    if let loc = spec.location, !loc.isEmpty {
      full(label(loc, size: Look.base, weight: .semibold, color: Look.accent))
    }

    var titleText = spec.title
    if let q = spec.question_label, !q.isEmpty { titleText = "［\(q)］ " + titleText }
    let titleLabel = richLabel(titleText, size: Look.title, weight: .bold)
    full(titleLabel)

    if let body = spec.body, !body.isEmpty {
      let commands = (spec.details ?? []).filter { ["コマンド", "対象コマンド"].contains($0.label) }.map { $0.value }
      let ranges = spec.kind == "command_guard" ? commandBodyRanges(body, commands: commands) : []
      func bodyLabel(_ text: String, offset: Int, weight: NSFont.Weight = .regular, color: NSColor = .labelColor) -> RichTextLabel {
        let slice = NSRange(location: offset, length: (text as NSString).length)
        let local = ranges.compactMap { range -> NSRange? in
          let part = NSIntersectionRange(slice, range)
          return part.length > 0 ? NSRange(location: part.location - offset, length: part.length) : nil
        }
        return richLabel(text, size: Look.base, weight: weight, color: color, commandRanges: local)
      }
      if spec.kind == "command_guard", (body.hasPrefix("ブロック理由：") || body.hasPrefix("承認が必要な理由:")),
        let boundary = body.range(of: "\n\n") {
        full(bodyLabel(String(body[..<boundary.lowerBound]), offset: 0, weight: .semibold, color: Look.accent))
        full(bodyLabel(String(body[boundary.upperBound...]), offset: (String(body[..<boundary.upperBound]) as NSString).length))
      } else {
        full(bodyLabel(body, offset: 0))
      }
    }

    richStack.orientation = .vertical
    richStack.alignment = .leading
    richStack.spacing = 8
    full(richStack)

    if let details = spec.details, !details.isEmpty {
      let grid = NSStackView()
      grid.orientation = .vertical
      grid.alignment = .leading
      grid.spacing = 3
      for d in details {
        if spec.kind == "command_guard", let body = spec.body {
          let normalizedBody = comparableApprovalDetail(body)
          let normalizedValue = comparableApprovalDetail(d.value)
          if d.label == "コマンド", body.contains("コマンド: ") { continue }
          if d.label == "対象コマンド", body.contains("対象コマンド：" + d.value) { continue }
          if ["理由", "場所"].contains(d.label), normalizedBody.contains(normalizedValue) { continue }
        }
        let row = label("\(d.label)：\(d.value)", size: Look.small, color: .secondaryLabelColor, mono: d.monospace == true)
        grid.addArrangedSubview(row)
        row.widthAnchor.constraint(equalTo: grid.widthAnchor).isActive = true
      }
      full(grid)
    }

    let fixedChoices = NSStackView()
    fixedChoices.orientation = .horizontal
    fixedChoices.distribution = .fillEqually
    fixedChoices.spacing = 8
    fixedChoices.translatesAutoresizingMaskIntoConstraints = false
    if !spec.options.isEmpty {
      let hint = spec.multi_select
        ? "当てはまるものを選んで（複数可・⌘1〜⌘\(min(spec.options.count, 9)) でも可）、右下の「回答する」か ⌘↩ で送ります。"
        : "1つ選んで（⌘1〜⌘\(min(spec.options.count, 9)) でも可）、右下の「回答する」か ⌘↩ で送ります。選んだだけでは送りません。もう一度押すと選択を外せます。"
      full(label(hint, size: Look.small, color: .secondaryLabelColor, selectable: false))
      for (i, option) in spec.options.enumerated() {
        let card = OptionCard(option: option, index: i, multi: spec.multi_select)
        card.onClick = { [weak self] c in self?.cardClicked(c) }
        cards.append(card)
        if spec.kind == "command_guard" {
          fixedChoices.addArrangedSubview(card)
        } else {
          full(card)
        }
      }
    }

    // Scrollable body so a long premise never pushes the buttons off screen.
    let scroll = NSScrollView()
    scroll.translatesAutoresizingMaskIntoConstraints = false
    scroll.hasVerticalScroller = true
    scroll.drawsBackground = false
    scroll.documentView = content
    content.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor).isActive = true

    // Bottom bar: free text, deadline and the two buttons stay put.
    let bottom = NSStackView()
    bottom.orientation = .vertical
    bottom.alignment = .leading
    bottom.spacing = 8
    statusLabel.textColor = .systemRed
    statusLabel.font = NSFont.systemFont(ofSize: Look.base)
    bottom.addArrangedSubview(statusLabel)
    statusLabel.widthAnchor.constraint(equalTo: bottom.widthAnchor, constant: -2 * Look.pad).isActive = true
    bottom.translatesAutoresizingMaskIntoConstraints = false
    bottom.edgeInsets = NSEdgeInsets(top: 12, left: Look.pad, bottom: 16, right: Look.pad)

    if spec.kind == "command_guard", !cards.isEmpty {
      bottom.addArrangedSubview(fixedChoices)
      fixedChoices.widthAnchor.constraint(equalTo: bottom.widthAnchor, constant: -2 * Look.pad).isActive = true
    }
    if spec.allow_text {
      let caption = label(spec.options.isEmpty
        ? "回答（⌘↩ で送信）\n画像を⌘V・ドラッグで添付できます（4枚・1枚20MBまで）。画像内の秘密は伏せ字になりません。"
        : "自由入力（任意。選んだ選択肢と一緒に送ります。文だけでも送れます）\n画像を⌘V・ドラッグで添付できます（4枚・1枚20MBまで）。画像内の秘密は伏せ字になりません。",
        size: Look.small, color: .secondaryLabelColor, selectable: false)
      bottom.addArrangedSubview(caption)
      let tv = FirstClickTextView()
      tv.font = NSFont.systemFont(ofSize: Look.base)
      tv.isRichText = false
      tv.allowsUndo = true
      tv.isAutomaticQuoteSubstitutionEnabled = false
      tv.textContainerInset = NSSize(width: 6, height: 6)
      tv.onCommandReturn = { [weak self] in self?.submit() }
      tv.delegate = self
      tv.registerForDraggedTypes([.fileURL, .png, .tiff])
      tv.onAttachments = { [weak self] board in self?.consumeAttachments(board) ?? false }
      let tscroll = NSScrollView()
      tscroll.translatesAutoresizingMaskIntoConstraints = false
      tscroll.hasVerticalScroller = true
      tscroll.borderType = .bezelBorder
      tscroll.documentView = tv
      tv.minSize = NSSize(width: 0, height: 0)
      tv.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
      tv.isVerticallyResizable = true
      tv.isHorizontallyResizable = false
      tv.autoresizingMask = [.width]
      tv.textContainer?.widthTracksTextView = true
      bottom.addArrangedSubview(tscroll)
      tscroll.heightAnchor.constraint(equalToConstant: 76).isActive = true
      tscroll.widthAnchor.constraint(equalTo: bottom.widthAnchor, constant: -2 * Look.pad).isActive = true
      textView = tv
      let attachButton = FirstClickButton(title: "画像を添付", target: self, action: #selector(chooseAttachments))
      attachButton.image = NSImage(systemSymbolName: "paperclip", accessibilityDescription: "画像を添付")
      attachButton.imagePosition = .imageLeading
      attachButton.bezelStyle = .rounded
      attachButton.toolTip = "画像は4枚・1枚20MBまで。画像の中の秘密情報は伏せ字になりません。"
      bottom.addArrangedSubview(attachButton)
      attachmentRow.orientation = .horizontal
      attachmentRow.spacing = 8
      bottom.addArrangedSubview(attachmentRow)
      attachmentRow.widthAnchor.constraint(equalTo: bottom.widthAnchor, constant: -2 * Look.pad).isActive = true
    }

    let laterLabel = spec.later_label ?? "10分"
    let laterButton = FirstClickButton(title: "あとで（\(laterLabel)後にもう一度）", target: self, action: #selector(later))
    laterButton.bezelStyle = .rounded
    laterButton.controlSize = .large
    laterButton.font = NSFont.systemFont(ofSize: Look.base)
    laterButton.toolTip = "答えずに閉じます。\(laterLabel)後にまだ答えていなければもう一度出します。Slack・アプリ・グラスではいつでも答えられます。（Esc）"

    submitButton = FirstClickButton(title: "回答する  ⌘↩", target: self, action: #selector(submit))
    submitButton.bezelStyle = .rounded
    submitButton.controlSize = .large
    submitButton.font = NSFont.systemFont(ofSize: Look.base, weight: .semibold)
    submitButton.keyEquivalent = "\r"
    submitButton.keyEquivalentModifierMask = [.command]

    var info: [String] = []
    if let d = spec.deadline, !d.isEmpty { info.append(d) }
    if let q = spec.queue_remaining, q > 0 { info.append("ほかに \(q) 件待っています") }
    infoLabel = label(info.joined(separator: "  ·  "), size: Look.small, color: .secondaryLabelColor, selectable: false)

    let row = NSStackView(views: [infoLabel, NSView(), laterButton, submitButton])
    row.orientation = .horizontal
    row.spacing = 10
    row.translatesAutoresizingMaskIntoConstraints = false
    bottom.addArrangedSubview(row)
    row.widthAnchor.constraint(equalTo: bottom.widthAnchor, constant: -2 * Look.pad).isActive = true

    let separator = NSBox()
    separator.boxType = .separator
    separator.translatesAutoresizingMaskIntoConstraints = false

    let root = KeyRootView()
    root.addSubview(scroll)
    root.addSubview(separator)
    root.addSubview(bottom)
    NSLayoutConstraint.activate([
      scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor),
      scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor),
      scroll.topAnchor.constraint(equalTo: root.topAnchor),
      separator.leadingAnchor.constraint(equalTo: root.leadingAnchor),
      separator.trailingAnchor.constraint(equalTo: root.trailingAnchor),
      separator.topAnchor.constraint(equalTo: scroll.bottomAnchor),
      bottom.leadingAnchor.constraint(equalTo: root.leadingAnchor),
      bottom.trailingAnchor.constraint(equalTo: root.trailingAnchor),
      bottom.topAnchor.constraint(equalTo: separator.bottomAnchor),
      bottom.bottomAnchor.constraint(equalTo: root.bottomAnchor),
    ])

    let screen = NSScreen.main?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
    panel = KeyPanel(
      contentRect: NSRect(x: 0, y: 0, width: Look.width, height: 400),
      styleMask: [.titled, .closable, .resizable],
      backing: .buffered, defer: false)
    panel.title = spec.window_title
    panel.contentView = root
    panel.delegate = self
    panel.level = .normal
    panel.hidesOnDeactivate = false
    panel.isFloatingPanel = false
    panel.becomesKeyOnlyIfNeeded = false
    panel.collectionBehavior = [.moveToActiveSpace]
    panel.minSize = NSSize(width: 520, height: 260)
    // Size: as tall as the content needs at the real width, capped to 85% of
    // the screen (the premise scrolls; the buttons never leave the window).
    root.layoutSubtreeIfNeeded()
    let contentHeight = stack.fittingSize.height + 32
    let bottomHeight = bottom.fittingSize.height
    if spec.kind == "command_guard" || spec.allow_text {
      let chromeHeight = panel.frame.height - panel.contentLayoutRect.height
      panel.minSize.height = max(panel.minSize.height, bottomHeight + chromeHeight)
    }
    let height = min(contentHeight + bottomHeight + 2, screen.height * 0.85)
    panel.setContentSize(NSSize(width: Look.width, height: height))
    panel.center()
    panel.setFrameTopLeftPoint(NSPoint(x: panel.frame.minX, y: screen.maxY - 60))
    if let draft = spec.draft {
      for card in cards { card.checked = draft.option_ids.contains(card.option.id) }
      textView?.string = draft.text
      attachments = draft.attachments ?? []
      refreshAttachments()
    }
    refreshSubmit()
  }

  func show() {
    // Ordering never activates the process or changes the keyboard target.
    // An explicitly selected Dock/Mission Control window can still become key.
    if spec.present_in_background == true { panel.orderBack(nil) }
    else { panel.orderFrontRegardless() }
    emit(["event": "shown", "pid": Int(ProcessInfo.processInfo.processIdentifier), "x": panel.frame.minX, "y": panel.frame.minY, "key_window": panel.isKeyWindow, "app_active": NSApp.isActive, "appearance": panel.effectiveAppearance.bestMatch(from: [.aqua, .darkAqua])?.rawValue ?? "unknown", "frontmost_pid": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
  }

  // Close with the decision: the daemon deletes the spec when it is resolved
  // elsewhere or when the daemon stops; a dead daemon also ends the panel.
  func respond(_ message: [String: Any]) {
    if message["status"] as? String == "front" {
      panel.orderFrontRegardless()
      return
    }
    if let status = message["status"] as? String, status.hasPrefix("rich_") { receiveRich(message); return }
    if message["status"] as? String == "error" {
          self.answerPending = false
          self.statusLabel.stringValue = message["message"] as? String ?? "回答の送信に失敗しました。"
          self.refreshSubmit()
          self.panel.contentView?.layoutSubtreeIfNeeded()
    } else if message["status"] as? String == "windows", let count = message["queue_remaining"] as? Int {
      var parts: [String] = []
      if let deadline = spec.deadline, !deadline.isEmpty { parts.append(deadline) }
      if count > 0 { parts.append("ほかに \(count) 件開いています") }
      infoLabel.stringValue = parts.joined(separator: "  ·  ")
    }
  }

  func startWatchdog() {
    Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
      guard let self else { return }
      if !FileManager.default.fileExists(atPath: self.specPath) {
        self.close()
      }
      if let token = self.spec.daemon_owner, let ownerPath = self.spec.daemon_ownership_path {
        let data = FileManager.default.contents(atPath: ownerPath)
        let owner = data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        if owner?["owner"] as? String != token || (owner?["pid"] as? Int) != Int(self.spec.daemon_pid ?? 0) {
          self.close()
        }
      }
      if let pid = self.spec.daemon_pid, pid > 0, kill(pid, 0) != 0, errno == ESRCH {
        self.close()
      }
    }
  }
}

extension DialogController: NSTextViewDelegate {
  func textDidChange(_ notification: Notification) { refreshSubmit(); saveDraft() }
}

// Rich content is constructed from the already-redacted display fields only.
func richLinkRanges(_ text: String) -> [NSTextCheckingResult] {
  guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else { return [] }
  return detector.matches(in: text, range: NSRange(text.startIndex..., in: text)).filter {
    guard let url = $0.url, let range = Range($0.range, in: text) else { return false }
    guard text[range].hasPrefix("http://") || text[range].hasPrefix("https://") else { return false }
    return ["http", "https"].contains(url.scheme?.lowercased() ?? "") && !url.absoluteString.contains("***")
  }
}
// Work in UTF-16 ranges, as NSAttributedString does, without changing display bytes.
func commandBodyRanges(_ text: String, commands: [String]) -> [NSRange] {
  var ranges: [NSRange] = []
  for heading in ["対象コマンド：", "コマンド: "] {
    var cursor = text.startIndex
    while cursor < text.endIndex, let label = text.range(of: heading, range: cursor..<text.endIndex) {
      cursor = label.upperBound
      guard label.lowerBound == text.startIndex || text[text.index(before: label.lowerBound)] == "\n" else { continue }
      for command in commands where !command.isEmpty {
        guard text[label.upperBound...].hasPrefix(command) else { continue }
        let end = text.index(label.upperBound, offsetBy: command.count)
        guard end == text.endIndex || text[end...].hasPrefix("\n\n") else { continue }
        ranges.append(NSRange(label.upperBound..<end, in: text))
        break
      }
    }
  }
  return ranges.sorted { $0.location < $1.location }
}

// An always-present text view receives a link's first click directly, without
// NSTextField first switching to its field editor.
final class RichTextLabel: NSTextView {
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override var needsPanelToBecomeKey: Bool { true }
  override func setFrameSize(_ size: NSSize) {
    let changed = frame.width != size.width
    super.setFrameSize(size)
    if changed { invalidateIntrinsicContentSize() }
  }
  override var intrinsicContentSize: NSSize {
    guard let container=textContainer, let manager=layoutManager else { return super.intrinsicContentSize }
    container.containerSize=NSSize(width:bounds.width > 0 ? bounds.width : Look.width - 2 * Look.pad,height:CGFloat.greatestFiniteMagnitude)
    manager.ensureLayout(for:container)
    return NSSize(width:NSView.noIntrinsicMetric,height:ceil(manager.usedRect(for:container).height))
  }
}
func richLabel(_ text: String, size: CGFloat, weight: NSFont.Weight = .regular, color: NSColor = .labelColor, selectable: Bool = true, commandRanges: [NSRange] = []) -> RichTextLabel {
  let field=RichTextLabel(frame:.zero)
  field.isEditable=false
  field.isSelectable=selectable
  field.isRichText=true
  field.drawsBackground=false
  field.textContainerInset = .zero
  field.textContainer?.lineFragmentPadding=0
  field.textContainer?.widthTracksTextView=true
  field.isHorizontallyResizable=false
  field.isVerticallyResizable=true
  field.translatesAutoresizingMaskIntoConstraints=false
  field.setContentCompressionResistancePriority(.required,for:.vertical)
  field.linkTextAttributes=[:]
  let font=NSFont.systemFont(ofSize:size,weight:weight)
  field.font=font;field.textColor=color
  let attributed=NSMutableAttributedString(string:text,attributes:[.font:font,.foregroundColor:color])
  for match in richLinkRanges(text) {
    attributed.addAttributes([.link:match.url!.absoluteString,.foregroundColor:NSColor.linkColor,.underlineStyle:NSUnderlineStyle.single.rawValue],range:match.range)
    field.isSelectable=true
  }
  for range in commandRanges {
    attributed.addAttributes([.font:NSFont.monospacedSystemFont(ofSize:size,weight:.regular),.foregroundColor:NSColor.systemRed,.underlineColor:NSColor.systemRed],range:range)
  }
  field.textStorage!.setAttributedString(attributed)
  return field
}
struct RichBitmap {
  let image: NSImage
  let width: Int
  let height: Int
  let ext: String
}
func readRegularImage(_ path: String, limit: Int) throws -> Data {
  let fd = open(path, O_RDONLY | O_NOFOLLOW | O_NONBLOCK)
  guard fd >= 0 else { throw CocoaError(.fileReadNoPermission) }
  let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
  defer { try? handle.close() }
  var info = stat()
  guard fstat(fd, &info) == 0, info.st_mode & S_IFMT == S_IFREG else { throw CocoaError(.fileReadUnsupportedScheme) }
  guard info.st_size <= limit else { throw CocoaError(.fileReadTooLarge) }
  let data = try handle.read(upToCount: limit + 1) ?? Data()
  guard data.count <= limit else { throw CocoaError(.fileReadTooLarge) }
  return data
}
func richBitmap(_ data: Data, attachment: Bool = false) throws -> RichBitmap {
  let limit = (attachment ? 20 : 10) * 1024 * 1024 // approved plan limits
  guard data.count <= limit, let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
    let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
    let width = properties[kCGImagePropertyPixelWidth] as? Int, let height = properties[kCGImagePropertyPixelHeight] as? Int,
    width > 0, height > 0, width <= 8000, height <= 8000,
    let type = CGImageSourceGetType(source), let ext = UTType(type as String)?.preferredFilenameExtension,
    ["png", "jpg", "jpeg", "gif", "webp"].contains(ext.lowercased()),
    let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [kCGImageSourceCreateThumbnailFromImageAlways: true, kCGImageSourceThumbnailMaxPixelSize: 640, kCGImageSourceCreateThumbnailWithTransform: true] as CFDictionary)
    else { throw NSError(domain: "ikesu", code: 1, userInfo: [NSLocalizedDescriptionKey: "画像の形式・容量・縦横8000画素の上限を確認してください。"] ) }
  return RichBitmap(image: NSImage(cgImage: image, size: .zero), width: width, height: height, ext: ext)
}
final class ImagePreviewButton: NSButton {
  var imagePath = ""
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  @objc func previewImage() {
    NSWorkspace.shared.open([URL(fileURLWithPath: imagePath)], withApplicationAt: URL(fileURLWithPath: "/System/Applications/Preview.app"), configuration: NSWorkspace.OpenConfiguration())
  }
  init(path: String, bitmap: RichBitmap, maxHeight: CGFloat = 160) {
    super.init(frame: .zero)
    imagePath = path
    image = bitmap.image
    imageScaling = .scaleProportionallyDown
    isBordered = false
    target = self; action = #selector(previewImage)
    translatesAutoresizingMaskIntoConstraints = false
    heightAnchor.constraint(equalToConstant: min(maxHeight, CGFloat(bitmap.height))).isActive = true
    let preferredWidth = widthAnchor.constraint(equalToConstant: min(640, maxHeight * CGFloat(bitmap.width) / CGFloat(bitmap.height)))
    preferredWidth.priority = .defaultHigh
    preferredWidth.isActive = true
    toolTip = "プレビューで開く"
  }
  required init?(coder: NSCoder) { fatalError() }
}
extension DialogController {
  @objc func chooseAttachments() {
    let picker = NSOpenPanel()
    picker.canChooseDirectories = false
    picker.allowsMultipleSelection = true
    picker.allowedContentTypes = [.png, .jpeg, .gif, .webP]
    picker.beginSheetModal(for: panel) { [weak self] result in
      if result == .OK { self?.attachFiles(picker.urls) }
    }
  }
  @discardableResult func consumeAttachments(_ board: NSPasteboard) -> Bool {
    guard spec.allow_text else { return false }
    if let urls = board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL], !urls.isEmpty { attachFiles(urls); return true }
    if let data = board.data(forType: .png) { stageAttachment(data: data, name: "貼り付け画像.png"); return true }
    if let data = board.data(forType: .tiff) { stageAttachment(data: data, clipboardTIFF: true, name: "貼り付け画像.png"); return true }
    return false
  }
  func attachFiles(_ urls: [URL]) {
    for url in urls { stageAttachment(file: url, name: url.lastPathComponent) }
  }
  func stageAttachment(file: URL? = nil, data: Data? = nil, clipboardTIFF: Bool = false, name: String) {
    guard spec.allow_text, !closed else { return }
    guard attachments.count + attachmentLoading < 4 else { statusLabel.stringValue = "添付は4枚までです。"; return }
    guard let directory = spec.draft_directory else { statusLabel.stringValue = "添付の保存先を用意できません。"; return }
    attachmentLoading += 1; refreshSubmit()
    imageWork.addOperation { [weak self] in
      guard let self, !self.closed else { return }
      do {
        var bytes: Data
        if let data { bytes = data } else if let file {
          bytes = try readRegularImage(file.path, limit: 20 * 1024 * 1024)
        } else { throw CocoaError(.fileReadUnknown) }
        if clipboardTIFF {
          guard bytes.count <= 20 * 1024 * 1024,
            let source = CGImageSourceCreateWithData(bytes as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
            let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
            let width = properties[kCGImagePropertyPixelWidth] as? Int, let height = properties[kCGImagePropertyPixelHeight] as? Int,
            width <= 8000, height <= 8000,
            let rep = NSBitmapImageRep(data: bytes), let png = rep.representation(using: .png, properties: [:]) else { throw CocoaError(.fileReadTooLarge) }
          bytes = png
        }
        let bitmap = try richBitmap(bytes, attachment: true)
        let destination = URL(fileURLWithPath: directory).appendingPathComponent(UUID().uuidString + "." + bitmap.ext)
        let fd = open(destination.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, S_IRUSR | S_IWUSR)
        guard fd >= 0 else { throw CocoaError(.fileWriteNoPermission) }
        let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        try handle.write(contentsOf: bytes); try handle.close()
        DispatchQueue.main.async {
          if self.closed { try? FileManager.default.removeItem(at: destination); return }
          self.attachmentLoading -= 1
          self.attachments.append(DialogAttachment(path: destination.path, name: name))
          self.refreshAttachments(); self.refreshSubmit(); self.saveDraft()
        }
      } catch {
        DispatchQueue.main.async { if !self.closed { self.attachmentLoading -= 1; self.statusLabel.stringValue = "画像を添付できません: \(error.localizedDescription)"; self.refreshSubmit() } }
      }
    }
  }
  func refreshAttachments() {
    for view in attachmentRow.arrangedSubviews { attachmentRow.removeArrangedSubview(view); view.removeFromSuperview() }
    for (index, attachment) in attachments.enumerated() {
      let group = NSStackView(); group.orientation = .vertical; group.spacing = 2
      let thumbnail = FirstClickImageView(); thumbnail.imageScaling = .scaleProportionallyDown
      thumbnail.translatesAutoresizingMaskIntoConstraints = false
      thumbnail.widthAnchor.constraint(equalToConstant: 72).isActive = true
      thumbnail.heightAnchor.constraint(equalToConstant: 48).isActive = true
      group.addArrangedSubview(thumbnail)
      imageWork.addOperation { [weak self, weak thumbnail] in
        guard let data = try? readRegularImage(attachment.path, limit: 20 * 1024 * 1024), let bitmap = try? richBitmap(data, attachment: true) else { return }
        DispatchQueue.main.async { if self?.closed == false { thumbnail?.image = bitmap.image } }
      }
      let remove = FirstClickButton(title: "\(index + 1) を外す", target: self, action: #selector(removeAttachment(_:)))
      remove.image = NSImage(systemSymbolName: "xmark", accessibilityDescription: "外す")
      remove.imagePosition = .imageLeading
      remove.tag = index; remove.toolTip = attachment.name; remove.bezelStyle = .rounded
      group.addArrangedSubview(remove); attachmentRow.addArrangedSubview(group)
    }
  }
  @objc func removeAttachment(_ sender: NSButton) {
    guard !answerPending, attachments.indices.contains(sender.tag) else { return }
    attachments.remove(at: sender.tag); refreshAttachments(); refreshSubmit(); saveDraft()
  }
  func receiveRich(_ message: [String: Any]) {
    guard !closed, let status = message["status"] as? String else { return }
    if status == "rich_error" { statusLabel.stringValue = message["message"] as? String ?? "画像を表示できません。"; return }
    if status == "rich_card", let title = message["title"] as? String, let url = message["url"] as? String {
      let group = NSStackView(); group.orientation = .vertical; group.alignment = .leading; group.spacing = 3
      let caption = richLabel(title + "\n" + (message["site"] as? String ?? "") + "\n" + url, size: Look.small)
      group.addArrangedSubview(caption)
      richStack.addArrangedSubview(group)
      group.widthAnchor.constraint(equalTo: richStack.widthAnchor).isActive = true
      caption.widthAnchor.constraint(equalTo: group.widthAnchor).isActive = true
      if let path = message["path"] as? String, !path.isEmpty { loadRichImage(path, into: group, height: 80) }
    } else if status == "rich_image", let path = message["path"] as? String { loadRichImage(path, into: richStack) }
  }
  func loadRichImage(_ path: String, into stack: NSStackView, height: CGFloat = 160) {
    imageWork.addOperation { [weak self, weak stack] in
      do {
        let data = try readRegularImage(path, limit: 10 * 1024 * 1024)
        let bitmap = try richBitmap(data)
        DispatchQueue.main.async {
          guard let self, !self.closed, let stack else { return }
          if !self.richWarningShown {
            self.richWarningShown = true
            let hint = label("画像内の秘密は伏せ字になりません。", size: Look.small, color: .secondaryLabelColor, selectable: false)
            self.richStack.insertArrangedSubview(hint, at: 0)
          }
          let button = ImagePreviewButton(path: path, bitmap: bitmap, maxHeight: height)
          stack.addArrangedSubview(button)
          button.widthAnchor.constraint(lessThanOrEqualTo: stack.widthAnchor).isActive = true
        }
      } catch { DispatchQueue.main.async { if self?.closed == false { self?.statusLabel.stringValue = "画像を表示できません: \(error.localizedDescription)" } } }
    }
  }
}
