import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

test('command body preserves UTF16 text, exact red ranges, monospace and links in light/dark', () => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-command-color-'));
  const dir=path.resolve('apps/mac-dialog-decisions');
  const source=fs.readFileSync(path.join(dir,'dialog.swift'),'utf8');
  const helper=source.slice(source.indexOf('func richLinkRanges('),source.indexOf('struct RichBitmap'));
  const program=`import AppKit
func label(_ text: String, size: CGFloat, weight: NSFont.Weight, color: NSColor, selectable: Bool) -> NSTextField {
 let f=NSTextField(labelWithString:text);f.font=NSFont.systemFont(ofSize:size,weight:weight);f.textColor=color;f.isSelectable=selectable;return f
}
${helper}
let app=NSApplication.shared
let text="ブロック理由：理由\\n対象コマンド：printf '猫🐈'\\n  https://example.com/a\\n\\nコマンド: bash test.sh\\n\\n申請文（外側から）\\n理由の説明 https://example.com/reason"
let expected=["printf '猫🐈'\\n  https://example.com/a", "bash test.sh"]
let ranges=commandBodyRanges(text)
precondition(ranges.count==2)
precondition(ranges.map {(text as NSString).substring(with:$0)}==expected)
for name in [NSAppearance.Name.aqua, .darkAqua] {
 app.appearance=NSAppearance(named:name)
 let f=richLabel(text,size:15,commandBody:true)
 precondition(f.stringValue==text && f.isSelectable)
 for n in 0..<(text as NSString).length {
  let inside=ranges.contains {NSLocationInRange(n,$0)}
  let attrs=f.attributedStringValue.attributes(at:n,effectiveRange:nil)
  precondition((attrs[.foregroundColor] as? NSColor == NSColor.systemRed)==inside,"red mismatch at \\(n)")
  if inside {precondition((attrs[.font] as! NSFont).isFixedPitch)}
 }
 precondition(f.attributedStringValue.attribute(.link,at:(text as NSString).range(of:"https://example.com/a").location,effectiveRange:nil) != nil)
 let normal=richLabel(text,size:15)
 precondition(normal.stringValue==text)
 let view=NSView(frame:NSRect(x:0,y:0,width:720,height:260));view.appearance=app.appearance
 f.frame=NSRect(x:20,y:20,width:680,height:220);view.addSubview(f)
 view.layoutSubtreeIfNeeded()
 let rep=view.bitmapImageRepForCachingDisplay(in:view.bounds)!
 view.cacheDisplay(in:view.bounds,to:rep)
 if let out=ProcessInfo.processInfo.environment["IKESU_COLOR_EVIDENCE"] {
  try rep.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:out+"/command-"+name.rawValue+".png"))
 }
}
precondition(commandBodyRanges("理由だけ").isEmpty)
precondition(commandBodyRanges("コマンド: ").isEmpty)
print("PASS exact_ranges=2 text_utf16=\\((text as NSString).length) appearances=2 selectable=1 links=1")
`;
  fs.writeFileSync(path.join(root,'main.swift'),program);
  const compile=spawnSync('swiftc',[path.join(root,'main.swift'),'-o',path.join(root,'check')],{encoding:'utf8'});
  assert.equal(compile.status,0,compile.stderr);
  const run=spawnSync(path.join(root,'check'),[],{encoding:'utf8',env:process.env});
  assert.equal(run.status,0,run.stdout+run.stderr);
  console.log(run.stdout.trim());
  fs.rmSync(root,{recursive:true});
});
