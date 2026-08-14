package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeBackendError   = -32000 // backend non-2xx response
	CodeFileSaveError  = -32001 // binary file save failure
)

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
	ID      string      `json:"id"`
}

func newRequestID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "req-0"
	}
	return "req-" + hex.EncodeToString(b)
}

// emit writes a JSON-RPC response to stdout.
func emit(r rpcResponse) {
	r.JSONRPC = "2.0"
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, "输出编码失败:", err)
	}
}

// PrintSuccess emits a success result (result kept as a native object, never stringified).
func PrintSuccess(id string, result interface{}) {
	emit(rpcResponse{Result: result, ID: id})
}

// PrintError emits an error with the given code/message/data.
func PrintError(id string, code int, message string, data interface{}) {
	emit(rpcResponse{Error: &rpcError{Code: code, Message: message, Data: data}, ID: id})
}
