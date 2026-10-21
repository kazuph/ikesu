package server

import (
 "bytes"
 "net"
 "path/filepath"
 "strings"
 "testing"
 "time"
 "github.com/kazuph/ikesu/internal/api"
 "github.com/kazuph/ikesu/internal/cli"
 "github.com/kazuph/ikesu/internal/teststate"
)

func TestPingAndStatusRespondWhileAppBlocked(t *testing.T) {
 teststate.Lock(t)
 s:=newTestServer(t,"busy-ping")
 path:=filepath.Join(teststate.TempDir(t),"gtx.sock")
 listener,err:=net.Listen("unix",path);if err!=nil{t.Fatal(err)}
 s.apiLn=listener
 teststate.Setenv(t,"GTX_SOCKET_PATH",path)
 apiDone,appDone:=make(chan struct{}),make(chan struct{})
 go func(){s.apiLoop();close(apiDone)}()
 go func(){s.app.Run();close(appDone)}()
 entered,release,unblocked:=make(chan struct{}),make(chan struct{}),make(chan struct{})
 go func(){s.app.Do(func(){close(entered);<-release});close(unblocked)}()
 <-entered
 defer func(){close(release);<-unblocked;s.app.Stop();<-appDone;_ = listener.Close();<-apiDone;s.apiWG.Wait();s.sup.KillAll()}()
 type result struct {code int;out,diagnostic string}
 invoke:=func(args []string)result{
  finished:=make(chan result,1)
  go func(){var out,diagnostic bytes.Buffer;code:=cli.Run(args,cli.Options{Stdout:&out,Stderr:&diagnostic});finished<-result{code,out.String(),diagnostic.String()}}()
  select{
  case r:=<-finished:return r
  case <-time.After(5*time.Second):t.Fatalf("%v waited on blocked app",args);return result{}
  }
 }
 ping:=invoke([]string{"server","ping"})
 if ping.code!=0 || !strings.Contains(ping.out,`"type":"pong"`){t.Fatal(ping)}
 status:=invoke([]string{"status","--json"})
 if status.code!=0 || !strings.Contains(status.out,`"running":true`){t.Fatal(status)}
 select{case <-unblocked:t.Fatal("app barrier unexpectedly released");default:}
 s.closing.Store(1)
 stopped:=invoke([]string{"server","ping"})
 if stopped.code==0 || !strings.Contains(stopped.diagnostic,"server_stopping"){t.Fatal("stopping ping accepted",stopped)}
 t.Log("real Unix ping/status completed while app barrier remained held; stopping ping returned server_stopping")
 // Verify the stopping result retains the caller's request ID as well.
 response:=s.dispatch(&api.Request{ID:"stopping-ping",Method:api.MethodPing})
 if response.ID!="stopping-ping" || response.Err==nil{t.Fatal(response)}
}
