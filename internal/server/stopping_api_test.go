package server

import (
 "bufio"
 "net"
 "path/filepath"
 "testing"
 "time"
 "github.com/kazuph/ikesu/internal/api"
 "github.com/kazuph/ikesu/internal/teststate"
)

func TestStoppedAppAPIRejectsInsteadOfZeroSuccess(t *testing.T){
 teststate.Lock(t)
 s:=newTestServer(t,"stopping-api")
 done:=make(chan struct{})
 go func(){s.app.Run();close(done)}()
 s.app.Stop();<-done
 defer s.sup.KillAll()
 path:=filepath.Join(teststate.TempDir(t),"stopping.sock")
 listener,err:=net.Listen("unix",path);if err!=nil{t.Fatal(err)};defer listener.Close()
 client,err:=net.Dial("unix",path);if err!=nil{t.Fatal(err)};defer client.Close()
 peer,err:=listener.Accept();if err!=nil{t.Fatal(err)}
 served:=make(chan struct{})
 s.apiWG.Add(1)
 go func(){s.serveAPI(peer);close(served)}()
 _=client.SetDeadline(time.Now().Add(5*time.Second))
 reader:=bufio.NewReader(client)
 for _,method:=range []string{api.MethodPaneList,api.MethodPing,"decision.list","plugin.list","agent.view.clear"}{
  req:=api.Request{ID:method,Method:method,Params:api.MarshalParams(api.EmptyParams{})}
  if err:=api.WriteRequest(client,req);err!=nil{t.Fatal(err)}
  response,err:=api.ReadResponse(reader)
  if err!=nil{t.Fatal(err)}
  if response.ID!=method || response.Err==nil{t.Errorf("stopped %s returned success/zero value: %+v",method,response)}
 }
 _=client.Close();<-served
}
