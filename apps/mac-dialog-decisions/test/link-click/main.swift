import AppKit
import Foundation
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
let resources=Bundle.main.resourceURL!
let config=try JSONSerialization.jsonObject(with:Data(contentsOf:resources.appendingPathComponent("fixture.json"))) as! [String:String]
let base=config["base"]!
let command="curl "+base+"/command"
let data=try JSONSerialization.data(withJSONObject:["window_title":"Ikesu リンク初回クリック試験", "kind":"command_guard", "title":"見出しリンク "+base+"/title", "body":"本文リンク "+base+"/body\n\nコマンド: "+command, "details":[["label":"コマンド","value":command]], "options":[["id":"close","label":"試験用の選択肢"]], "multi_select":false,"allow_text":false,"draft_directory":resources.path])
let spec=try JSONDecoder().decode(DialogSpec.self,from:data)
let controller=DialogController(spec:spec,specPath:resources.appendingPathComponent("spec.json").path)
DispatchQueue.main.async {controller.build();controller.show()}
app.run()
