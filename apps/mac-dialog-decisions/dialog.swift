// AppKit controller for one decision window; DialogHost owns process lifetime.
// New windows appear once without taking focus. Selection requires explicit submit.
// Draft, result, shown and closed events carry the window key.

import AppKit
import Foundation

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
}

struct DialogDraft: Decodable { let option_ids: [String]; let text: String }

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

final class FirstClickTextView: NSTextView {
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
// The process only becomes active when the user clicks into the panel (never
// on its own): text input — including Japanese input methods — needs an
// active app, and a click is the user's own choice to start answering.
final class KeyPanel: NSPanel {
  override var canBecomeKey: Bool { true }
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
  private let mark = NSTextField(labelWithString: "")
  private let box = NSImageView()

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
      stack.addArrangedSubview(label(d, size: Look.small + 1, color: .secondaryLabelColor, selectable: false))
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
    emit(["event": "draft", "draft": ["option_ids": checkedIds(), "text": textView?.string ?? ""]])
  }

  func close() {
    if closed { return }
    closed = true
    saveDraft()
    panel.orderOut(nil)
    panel.close()
    emit(["event": "closed"])
    onClose?(self)
  }

  func answer(ids: [String]) {
    guard !answerPending else { return }
    answerPending = true
    statusLabel.stringValue = "回答を送信中…"
    refreshSubmit()
    emit(["result": "answer", "option_ids": ids, "text": currentText()])
  }

  @objc func submit() {
    let ids = checkedIds()
    let text = currentText()
    if ids.isEmpty && text.isEmpty {
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
    let ready = !checkedIds().isEmpty || !currentText().isEmpty
    submitButton.isEnabled = ready && !answerPending
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
      if NSApp.sendAction(action, to: nil, from: panel) { return nil }
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
    full(label(titleText, size: Look.title, weight: .bold))

    if let body = spec.body, !body.isEmpty {
      full(label(body, size: Look.base))
    }

    if let details = spec.details, !details.isEmpty {
      let grid = NSStackView()
      grid.orientation = .vertical
      grid.alignment = .leading
      grid.spacing = 3
      for d in details {
        let row = label("\(d.label)：\(d.value)", size: Look.small, color: .secondaryLabelColor, mono: d.monospace == true)
        grid.addArrangedSubview(row)
        row.widthAnchor.constraint(equalTo: grid.widthAnchor).isActive = true
      }
      full(grid)
    }

    if !spec.options.isEmpty {
      let hint = spec.multi_select
        ? "当てはまるものを選んで（複数可・⌘1〜⌘\(min(spec.options.count, 9)) でも可）、右下の「回答する」か ⌘↩ で送ります。"
        : "1つ選んで（⌘1〜⌘\(min(spec.options.count, 9)) でも可）、右下の「回答する」か ⌘↩ で送ります。選んだだけでは送りません。もう一度押すと選択を外せます。"
      full(label(hint, size: Look.small, color: .secondaryLabelColor, selectable: false))
      for (i, option) in spec.options.enumerated() {
        let card = OptionCard(option: option, index: i, multi: spec.multi_select)
        card.onClick = { [weak self] c in self?.cardClicked(c) }
        cards.append(card)
        full(card)
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

    if spec.allow_text {
      let caption = label(spec.options.isEmpty
        ? "回答（⌘↩ で送信）"
        : "自由入力（任意。選んだ選択肢と一緒に送ります。文だけでも送れます）",
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
      styleMask: [.titled, .closable, .resizable, .nonactivatingPanel],
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
    let height = min(contentHeight + bottomHeight + 2, screen.height * 0.85)
    panel.setContentSize(NSSize(width: Look.width, height: height))
    panel.center()
    panel.setFrameTopLeftPoint(NSPoint(x: panel.frame.minX, y: screen.maxY - 60))
    if let draft = spec.draft {
      for card in cards { card.checked = draft.option_ids.contains(card.option.id) }
      textView?.string = draft.text
    }
    refreshSubmit()
  }

  func show() {
    // Shown once, on top, without activating ikesu's process or taking the
    // keyboard from whatever the user is doing.
    panel.orderFrontRegardless()
    emit(["event": "shown", "pid": Int(ProcessInfo.processInfo.processIdentifier), "x": panel.frame.minX, "y": panel.frame.minY, "key_window": panel.isKeyWindow, "app_active": NSApp.isActive, "appearance": panel.effectiveAppearance.bestMatch(from: [.aqua, .darkAqua])?.rawValue ?? "unknown", "frontmost_pid": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
  }

  // Close with the decision: the daemon deletes the spec when it is resolved
  // elsewhere or when the daemon stops; a dead daemon also ends the panel.
  func respond(_ message: [String: Any]) {
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
