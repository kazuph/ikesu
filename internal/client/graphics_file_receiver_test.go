package client

import (
	"os"
	"strings"

	"testing"
	"time"
)

func graphicsReceiverOutput(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "terminal-output")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestGraphicsFileReceiverOrdersFastReplyAfterStarted(t *testing.T) {
	var receiver graphicsFileReceiver
	now := time.Now()
	identity, err := receiver.begin(graphicsReceiverOutput(t), graphicsTransferFixture(t), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if consumed, result := receiver.reply([]byte("\x1b_Gi=17;OK\x1b\\"), now); !consumed || result != nil {
		t.Fatal("fast reply escaped before Started", consumed, result)
	}
	wrong := identity
	wrong.TransferID++
	if result := receiver.started(wrong, now); result != nil {
		t.Fatal("another transfer released the early result", result)
	}
	result := receiver.started(identity, now)
	if result == nil || result.GraphicsTransmission != identity || !result.Success {
		t.Fatal("lost the real early success", result)
	}
	if consumed, result := receiver.reply([]byte("\x1b_Gi=17;OK\x1b\\"), now); !consumed || result != nil {
		t.Fatal("duplicate result was not drained", consumed, result)
	}
	if consumed, _ := receiver.reply([]byte("a"), now); consumed {
		t.Fatal("drain consumed ordinary input")
	}
	if consumed, _ := receiver.reply([]byte("\x1b_Gi=17;OK\x1b\\"), now.Add(graphicsFileReplyDrain)); consumed {
		t.Fatal("drain retained a reply beyond the specified lifetime")
	}
}

func TestGraphicsFileReceiverRetirementCannotAcknowledgeOrCrossTransfers(t *testing.T) {
	var receiver graphicsFileReceiver
	now := time.Now()
	message := graphicsTransferFixture(t)
	out := graphicsReceiverOutput(t)
	identity, err := receiver.begin(out, message, true, now)
	if err != nil {
		t.Fatal(err)
	}
	receiver.started(identity, now)
	wrong := identity
	wrong.TransferID++
	receiver.retired(wrong, now)
	receiver.retired(identity, now)
	if consumed, result := receiver.reply([]byte("\x1b_Gi=17;OK\x1b\\"), now); !consumed || result != nil {
		t.Fatal("retired reply acknowledged the producer", consumed, result)
	}
	if _, err := receiver.begin(out, message, true, now); err == nil {
		t.Fatal("reused an image ID while its old reply was being drained")
	}
	message.ImageID, message.TransferID = 18, 10
	message.Control = strings.Replace(message.Control, "i=17", "i=18", 1)
	next, err := receiver.begin(out, message, true, now)
	if err != nil {
		t.Fatal(err)
	}
	receiver.started(next, now)
	if consumed, result := receiver.reply([]byte("\x1b_Gi=17;OK\x1b\\"), now); !consumed || result != nil {
		t.Fatal("stale image reply completed the new transfer", consumed, result)
	}
	if consumed, result := receiver.reply([]byte("\x1b_Gi=18;ENOENT\x1b\\"), now); !consumed || result == nil || result.GraphicsTransmission != next || result.Success {
		t.Fatal("new transfer did not retain its actual rejection", consumed, result)
	}
}
