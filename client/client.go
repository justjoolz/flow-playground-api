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

// Package client is used internally for testing. See readme for alternatives
//
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/mitchellh/mapstructure"
)

type (
	// Client used for testing GraphQL servers. Not for production use.
	Client struct {
		h    http.Handler
		opts []Option
	}

	// Option implements a visitor that mutates an outgoing GraphQL request
	//
	// This is the Option pattern - https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis
	Option func(bd *Request)

	// Request represents an outgoing GraphQL request
	Request struct {
		Query         string                 `json:"query"`
		Variables     map[string]interface{} `json:"variables,omitempty"`
		OperationName string                 `json:"operationName,omitempty"`
		HTTP          *http.Request          `json:"-"`
	}

	// Response is a GraphQL layer response from a handler.
	Response struct {
		Data       interface{}
		Errors     json.RawMessage
		Extensions map[string]interface{}
	}
)

// New creates a graphql client
// Options can be set that should be applied to all requests made with this client
func New(h http.Handler, opts ...Option) *Client {
	p := &Client{
		h:    h,
		opts: opts,
	}

	return p
}

// Post sends a http POST request to the graphql endpoint with the given query then unpacks
// the response into the given object.
func (p *Client) Post(w *httptest.ResponseRecorder, query string, response interface{}, options ...Option) error {
	respDataRaw, err := p.RawPost(w, query, options...)
	if err != nil {
		return err
	}

	// we want to unpack even if there is an error, so we can see partial responses
	unpackErr := unpack(respDataRaw.Data, response)

	if respDataRaw.Errors != nil {
		return RawJsonError{respDataRaw.Errors}
	}
	return unpackErr
}

// RawPost is similar to Post, except it skips decoding the raw json response
// unpacked onto Response. This is used to test extension keys which are not
// available when using Post.
func (p *Client) RawPost(w *httptest.ResponseRecorder, query string, options ...Option) (*Response, error) {
	r, err := p.newRequest(query, options...)
	if err != nil {
		return nil, fmt.Errorf("build: %s", err.Error())
	}

	p.h.ServeHTTP(w, r)

	if w.Code >= http.StatusBadRequest {
		return nil, fmt.Errorf("http %d: %s", w.Code, w.Body.String())
	}

	// decode it into map string first, let mapstructure do the final decode
	// because it can be much stricter about unknown fields.
	respDataRaw := &Response{}
	err = json.Unmarshal(w.Body.Bytes(), &respDataRaw)
	if err != nil {
		return nil, fmt.Errorf("decode: %s", err.Error())
	}

	return respDataRaw, nil
}

func (p *Client) newRequest(query string, options ...Option) (*http.Request, error) {
	bd := &Request{
		Query: query,
		HTTP:  httptest.NewRequest(http.MethodPost, "/", nil),
	}
	bd.HTTP.Header.Set("Content-Type", "application/json")

	// per client options from client.New apply first
	for _, option := range p.opts {
		option(bd)
	}
	// per request options
	for _, option := range options {
		option(bd)
	}

	switch bd.HTTP.Header.Get("Content-Type") {
	case "application/json":
		requestBody, err := json.Marshal(bd)
		if err != nil {
			return nil, fmt.Errorf("encode: %s", err.Error())
		}
		bd.HTTP.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))
	default:
		panic("unsupported encoding" + bd.HTTP.Header.Get("Content-Type"))
	}

	return bd.HTTP, nil
}

func unpack(data interface{}, into interface{}) error {
	d, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      into,
		TagName:     "json",
		ErrorUnused: true,
		ZeroFields:  true,
	})
	if err != nil {
		return fmt.Errorf("mapstructure: %s", err.Error())
	}

	return d.Decode(data)
}
