package layout

import (
	"bytes"
	"encoding/gob"
)

// The display transport needs the tree, focus and creation order, including
// fields that are deliberately private to the layout package.
func (l *Layout) GobEncode() ([]byte, error) {
	var data bytes.Buffer
	err := gob.NewEncoder(&data).Encode(layoutWire{l.root, l.focus, l.order})
	return data.Bytes(), err
}

func (l *Layout) GobDecode(data []byte) error {
	var value layoutWire
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&value); err != nil {
		return err
	}
	l.root, l.focus, l.order = value.Root, value.Focus, value.Order
	return nil
}

type layoutWire struct {
	Root  *Node
	Focus int
	Order []int
}
