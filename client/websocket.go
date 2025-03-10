/*
 * Flow Playground
 *
 * Copyright 2019-2021 Dapper Labs, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"strings"

	"github.com/gorilla/websocket"
)

const (
	connectionInitMsg = "connection_init" // Client -> Server
	startMsg          = "start"           // Client -> Server
	connectionAckMsg  = "connection_ack"  // Server -> Client
	connectionKaMsg   = "ka"              // Server -> Client
	dataMsg           = "data"            // Server -> Client
	errorMsg          = "error"           // Server -> Client
)

type operationMessage struct {
	Payload json.RawMessage `json:"payload,omitempty"`
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
}

type Subscription struct {
	Close func() error
	Next  func(response interface{}) error
}

func errorSubscription(err error) *Subscription {
	return &Subscription{
		Close: func() error { return nil },
		Next: func(response interface{}) error {
			return err
		},
	}
}

func (p *Client) Websocket(query string, options ...Option) *Subscription {
	return p.WebsocketWithPayload(query, nil, options...)
}

// Grab a single response from a websocket based query
func (p *Client) WebsocketOnce(query string, resp interface{}, options ...Option) error {
	sock := p.Websocket(query)
	defer sock.Close()
	return sock.Next(&resp)
}

func (p *Client) WebsocketWithPayload(query string, initPayload map[string]interface{}, options ...Option) *Subscription {
	r, err := p.newRequest(query, options...)
	if err != nil {
		return errorSubscription(fmt.Errorf("request: %s", err.Error()))
	}

	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return errorSubscription(fmt.Errorf("parse body: %s", err.Error()))
	}

	srv := httptest.NewServer(p.h)
	host := strings.Replace(srv.URL, "http://", "ws://", -1)
	c, _, err := websocket.DefaultDialer.Dial(host+r.URL.Path, r.Header)

	if err != nil {
		return errorSubscription(fmt.Errorf("dial: %s", err.Error()))
	}

	initMessage := operationMessage{Type: connectionInitMsg}
	if initPayload != nil {
		initMessage.Payload, err = json.Marshal(initPayload)
		if err != nil {
			return errorSubscription(fmt.Errorf("parse payload: %s", err.Error()))
		}
	}

	if err = c.WriteJSON(initMessage); err != nil {
		return errorSubscription(fmt.Errorf("init: %s", err.Error()))
	}

	var ack operationMessage
	if err = c.ReadJSON(&ack); err != nil {
		return errorSubscription(fmt.Errorf("ack: %s", err.Error()))
	}

	if ack.Type != connectionAckMsg {
		return errorSubscription(fmt.Errorf("expected ack message, got %#v", ack))
	}

	var ka operationMessage
	if err = c.ReadJSON(&ka); err != nil {
		return errorSubscription(fmt.Errorf("ack: %s", err.Error()))
	}

	if ka.Type != connectionKaMsg {
		return errorSubscription(fmt.Errorf("expected ack message, got %#v", ack))
	}

	if err = c.WriteJSON(operationMessage{Type: startMsg, ID: "1", Payload: requestBody}); err != nil {
		return errorSubscription(fmt.Errorf("start: %s", err.Error()))
	}

	return &Subscription{
		Close: func() error {
			srv.Close()
			return c.Close()
		},
		Next: func(response interface{}) error {
			var op operationMessage
			err := c.ReadJSON(&op)
			if err != nil {
				return err
			}
			if op.Type != dataMsg {
				if op.Type == errorMsg {
					return fmt.Errorf(string(op.Payload))
				} else {
					return fmt.Errorf("expected data message, got %#v", op)
				}
			}

			var respDataRaw Response
			err = json.Unmarshal(op.Payload, &respDataRaw)
			if err != nil {
				return fmt.Errorf("decode: %s", err.Error())
			}

			// we want to unpack even if there is an error, so we can see partial responses
			unpackErr := unpack(respDataRaw.Data, response)

			if respDataRaw.Errors != nil {
				return RawJsonError{respDataRaw.Errors}
			}
			return unpackErr
		},
	}
}
