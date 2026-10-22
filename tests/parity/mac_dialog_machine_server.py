import json,os,pathlib,subprocess,sys,time
root=pathlib.Path(sys.argv[1]);root.mkdir()
exe=sys.argv[2]
env=os.environ.copy();env.update(HOME=str(root),IKESU_CONFIG=str(root/'config.toml'),IKESU_STATE_DIR=str(root/'state'),IKESU_SOCKET_PATH=str(root/'state/default/ikesu.sock'),IKESU_SOCKET_PATH_EXPLICIT='1')
(root/'config.toml').write_text('')
log=(root/'server.log').open('a');server=None
def up():
 global server
 server=subprocess.Popen([exe,'server'],env=env,stdout=log,stderr=log)
 for _ in range(200):
  result=subprocess.run([exe,'server','ping'],env=env,capture_output=True)
  if result.returncode==0:print('UP',flush=True);return
  if server.poll()is not None:raise RuntimeError((root/'server.log').read_text())
  time.sleep(.05)
 raise RuntimeError('startup deadline')
def down():
 global server
 subprocess.run([exe,'server','stop'],env=env,capture_output=True,timeout=15)
 server.wait(timeout=15);server=None;print('DOWN',flush=True)
try:
 up()
 for command in sys.stdin:
  if command.strip()=='down':down()
  elif command.strip()=='up':up()
  else:break
finally:
 if server is not None:down()
