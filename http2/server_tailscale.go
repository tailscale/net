// Copyright 2021 Tailscale. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http2

import (
	"expvar"
	"fmt"
	"strconv"
)

// http2ErrorCounters is a counter with name fitting the conventions used by
// tailscale.com/tsweb to format Prometheus metrics from an expvar.Map.
// The label is "type" (the error type), it's of metric type "counter" (it only goes up),
// and the metric name is "http2_server_errors".
var http2ErrorCounters = expvar.NewMap("counter_labelmap_type_http2_server_errors")

func countError(name string, err error) error {
	var typ string
	var code ErrCode
	switch e := err.(type) {
	case ConnectionError:
		typ = "conn"
		code = ErrCode(e)
	case StreamError:
		typ = "stream"
		code = ErrCode(e.Code)
	default:
		return err
	}
	codeStr := errCodeName[code]
	if codeStr == "" {
		codeStr = strconv.Itoa(int(code))
	}
	http2ErrorCounters.Add(fmt.Sprintf("%s_%s_%s", typ, codeStr, name), 1)
	return err
}
