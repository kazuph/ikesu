package server

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

func encode64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func decode64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

// readClientMsg reads one newline-delimited client message.
func readClientMsg(r *bufio.Reader) (clientMsg, error) {
	var m clientMsg
	line, err := r.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("invalid client message: %w", err)
	}
	return m, nil
}
