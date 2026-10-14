package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/machine"
)

func (m *machineClient) sidebarRequests(id string, requests []api.Request) {
	e := m.endpoints[id]
	ctx, profile, attempt := e.ctx, e.profile, e.attempt
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		for _, request := range requests {
			var stream io.ReadWriteCloser
			var err error
			if id == "local" {
				stream, err = (&net.Dialer{}).DialContext(ctx, "unix", api.APISocketPath(api.SessionDir(profile.Session)))
			} else {
				stream, err = openSSHEndpoint(profile, func(url string) { m.post(ctx, endpointEvent{id: id, attempt: attempt, approval: url}) })
			}
			if err == nil {
				stop := context.AfterFunc(ctx, func() { stream.Close() })
				err = sidebarRequest(stream, request)
				if err == nil && id != "local" {
					m.post(ctx, endpointEvent{formUpdate: func() {
						if err := machine.ClearApproval(profile.ID); err != nil {
							m.notify(err.Error())
						}
					}})
				}
				stop()
				stream.Close()
			}
			if err != nil {
				m.post(ctx, endpointEvent{formUpdate: func() { m.notify(fmt.Sprintf("%s: %s: %v", profile.Label, request.Method, err)) }})
				return
			}
		}
	}()
}

func sidebarRequest(stream io.ReadWriter, request api.Request) error {
	if err := api.WriteRequest(stream, request); err != nil {
		return err
	}
	response, err := api.ReadResponse(bufio.NewReader(stream))
	if err != nil {
		return fmt.Errorf("sidebar API response: %w", err)
	}
	if response.ID != request.ID {
		return fmt.Errorf("sidebar API response ID mismatch")
	}
	if response.Err != nil {
		return response.Err
	}
	return nil
}
