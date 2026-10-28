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
${source.slice(source.indexOf('enum Look {'), source.indexOf('\n// Match the guard'))}
${helper}
let app=NSApplication.shared
let text="ブロック理由：理由\\n対象コマンド：printf '猫🐈'\\n\\n  https://example.com/a\\n\\nコマンド: bash test.sh\\n\\n申請文（外側から）\\n理由の説明 https://example.com/reason"
let expected=["printf '猫🐈'\\n\\n  https://example.com/a", "bash test.sh"]
let ranges=commandBodyRanges(text, commands:expected)
precondition(ranges.count==2)
precondition(ranges.map {(text as NSString).substring(with:$0)}==expected)
for name in [NSAppearance.Name.aqua, .darkAqua] {
 app.appearance=NSAppearance(named:name)
 let f=richLabel(text,size:15,commandRanges:ranges)
 precondition(f.string==text && f.isSelectable)
 for n in 0..<(text as NSString).length {
  let inside=ranges.contains {NSLocationInRange(n,$0)}
  let attrs=f.attributedString().attributes(at:n,effectiveRange:nil)
  precondition((attrs[.foregroundColor] as? NSColor == NSColor.systemRed)==inside,"red mismatch at \\(n)")
  if inside {precondition((attrs[.font] as! NSFont).isFixedPitch)}
 }
 precondition(f.attributedString().attribute(.link,at:(text as NSString).range(of:"https://example.com/a").location,effectiveRange:nil) != nil)
 precondition(f.attributedString().attribute(.link,at:(text as NSString).range(of:"https://example.com/reason").location,effectiveRange:nil) != nil)
 let normal=richLabel(text,size:15)
 precondition(normal.string==text)
 let view=NSView(frame:NSRect(x:0,y:0,width:720,height:260));view.appearance=app.appearance
 f.frame=NSRect(x:20,y:20,width:680,height:220);view.addSubview(f)
 view.layoutSubtreeIfNeeded()
 let rep=view.bitmapImageRepForCachingDisplay(in:view.bounds)!
 view.cacheDisplay(in:view.bounds,to:rep)
 if let out=ProcessInfo.processInfo.environment["IKESU_COLOR_EVIDENCE"] {
  try rep.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:out+"/command-"+name.rawValue+".png"))
 }
}
precondition(commandBodyRanges("理由だけ",commands:expected).isEmpty)
precondition(commandBodyRanges("コマンド: ",commands:expected).isEmpty)
for reason in ["検証用の送信です", "検証用の送信です https://example.test/help"] {
 let body="コマンド: curl https://example.test/command\\n\\n"+reason
 let command="curl https://example.test/command"
 let exact=commandBodyRanges(body,commands:[command])
 precondition(exact.count==1 && (body as NSString).substring(with:exact[0])==command)
 precondition(commandBodyRanges(body,commands:["curl other"]).isEmpty)
 let field=richLabel(body,size:15,commandRanges:exact)
 let reasonRange=(body as NSString).range(of:reason)
 for n in reasonRange.location..<NSMaxRange(reasonRange) {
  precondition(field.attributedString().attribute(.foregroundColor,at:n,effectiveRange:nil) as? NSColor != .systemRed)
 }
 if reason.contains("https://") {precondition(field.attributedString().attribute(.link,at:(body as NSString).range(of:"https://example.test/help").location,effectiveRange:nil) != nil)}
}
print("PASS exact_ranges=2 text_utf16=\\((text as NSString).length) appearances=2 selectable=1 proseLinks=1 commandLinks=1")
`;
  fs.writeFileSync(path.join(root,'main.swift'),program);
  const compile=spawnSync('swiftc',[path.join(root,'main.swift'),'-o',path.join(root,'check')],{encoding:'utf8'});
  assert.equal(compile.status,0,compile.stderr);
  const run=spawnSync(path.join(root,'check'),[],{encoding:'utf8',env:process.env});
  assert.equal(run.status,0,run.stdout+run.stderr);
  console.log(run.stdout.trim());
  fs.rmSync(root,{recursive:true});
});
