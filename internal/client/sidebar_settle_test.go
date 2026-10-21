package client

import (
 "fmt"
 "testing"
)

func sidebarDragSettled(m *machineClient,right bool,width int)bool{
 s:=m.endpoints[m.active].snapshot.State
 if right{return s.RightSidebarArea().W==width}
 return s.SidebarArea().W==width
}

func TestSidebarMotionSnapshotDoesNotCompleteRelease(t *testing.T){
 h:=newRightMachineHarness(t)
 h.modes(0,0,240,60)
 m:=h.m
 start:=m.endpoints[m.active].snapshot.State.SidebarArea().W-1
 for _,raw:=range []string{fmt.Sprintf("\x1b[<0;%d;6M",start+1),"\x1b[<32;2;6M"}{
  if err:=m.input(mouseEvent(t,raw));err!=nil{t.Fatal(err)}
 }
 released:=false
 defer func(){if !released{_ = m.input(mouseEvent(t,"\x1b[<0;2;6m"))}}()
 h.until("real drag motion snapshot",func()bool{return m.endpoints[m.active].snapshot.State.SidebarArea().W==2})
 if sidebarDragSettled(m,false,2){t.Fatal("motion snapshot accepted before release and saved preferences")}
 if err:=m.input(mouseEvent(t,"\x1b[<0;2;6m"));err!=nil{t.Fatal(err)}
 released=true
 h.until("real release preference round trip",func()bool{return sidebarDragSettled(m,false,2)})
 t.Log("real motion snapshot preceded release; completion required persisted preference round trip")
}
