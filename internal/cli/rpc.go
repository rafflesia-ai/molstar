package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func (a app) rpcCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Call a running Mol* server over JSON-RPC",
	}
	cmd.AddCommand(a.rpcCapabilitiesCommand())
	cmd.AddCommand(a.rpcMetricsCommand())
	cmd.AddCommand(a.rpcExplainCommand())
	cmd.AddCommand(a.rpcValidateCommand())
	cmd.AddCommand(a.rpcRenderCommand())
	cmd.AddCommand(a.rpcRawCommand())
	return cmd
}

func (a app) rpcCapabilitiesCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Fetch server capabilities via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc capabilities", flags.jsonReport, func() error {
				if err := exactArgs(args, 0, "rpc capabilities"); err != nil {
					return markError(kindInvalidInput, err)
				}
				return a.callRPC(cmd.Context(), flags, "capabilities", nil)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) rpcMetricsCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Fetch server metrics via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc metrics", flags.jsonReport, func() error {
				if err := exactArgs(args, 0, "rpc metrics"); err != nil {
					return markError(kindInvalidInput, err)
				}
				return a.callRPC(cmd.Context(), flags, "metrics", nil)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) rpcExplainCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "explain JOB",
		Short: "Explain a job or recipe via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc explain", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "rpc explain"); err != nil {
					return markError(kindInvalidInput, err)
				}
				params, err := a.rpcJobParams(args[0], map[string]any{})
				if err != nil {
					return err
				}
				return a.callRPC(cmd.Context(), flags, "explain", params)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) rpcValidateCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	var strict bool
	cmd := &cobra.Command{
		Use:   "validate JOB",
		Short: "Validate a job or recipe via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc validate", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "rpc validate"); err != nil {
					return markError(kindInvalidInput, err)
				}
				params, err := a.rpcJobParams(args[0], map[string]any{"strict": strict})
				if err != nil {
					return err
				}
				return a.callRPC(cmd.Context(), flags, "validate", params)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	cmd.Flags().BoolVar(&strict, "strict", false, "use strict schema validation on the server")
	return cmd
}

func (a app) rpcRenderCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	var dryRun bool
	var async bool
	cmd := &cobra.Command{
		Use:   "render JOB",
		Short: "Render a job or recipe via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc render", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "rpc render"); err != nil {
					return markError(kindInvalidInput, err)
				}
				params, err := a.rpcJobParams(args[0], map[string]any{"dry_run": dryRun, "async": async})
				if err != nil {
					return err
				}
				return a.callRPC(cmd.Context(), flags, "render", params)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "ask the server to plan rendering without running the renderer")
	cmd.Flags().BoolVar(&async, "async", false, "return as soon as the server accepts the render job")
	return cmd
}

func (a app) rpcRawCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	var params string
	cmd := &cobra.Command{
		Use:   "raw METHOD",
		Short: "Call an arbitrary JSON-RPC method",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("rpc raw", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "rpc raw"); err != nil {
					return markError(kindInvalidInput, err)
				}
				raw, err := a.rpcRawParams(params)
				if err != nil {
					return err
				}
				return a.callRPC(cmd.Context(), flags, args[0], raw)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	cmd.Flags().StringVar(&params, "params", "", "JSON params object, @file path, or - for stdin")
	return cmd
}

func (a app) rpcJobParams(path string, extra map[string]any) (json.RawMessage, error) {
	data, name, err := a.readInput(path)
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	j, err := a.loadJobOrRecipeBytes(data, name, true)
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	params := map[string]any{"job": j}
	for key, value := range extra {
		params[key] = value
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, markError(kindInternal, err)
	}
	return raw, nil
}

func (a app) rpcRawParams(value string) (json.RawMessage, error) {
	if value == "" {
		return nil, nil
	}
	var data []byte
	var err error
	switch {
	case value == "-":
		data, _, err = a.readInput("-")
	case len(value) > 1 && value[0] == '@':
		data, _, err = a.readInput(value[1:])
	default:
		data = []byte(value)
	}
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	if !json.Valid(data) {
		return nil, markError(kindInvalidInput, fmt.Errorf("--params must be valid JSON"))
	}
	return json.RawMessage(data), nil
}

func (a app) callRPC(ctx context.Context, flags *serverClientFlags, method string, params json.RawMessage) error {
	request := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      json.RawMessage("1"),
	}
	if len(params) > 0 {
		request.Params = params
	}
	body, err := json.Marshal(request)
	if err != nil {
		return markError(kindInternal, err)
	}
	response, status, err := serverClientRequest(ctx, flags, http.MethodPost, "/rpc", body)
	if err != nil {
		return err
	}
	return a.writeRPCResponse(response, status, flags.jsonReport)
}

func (a app) writeRPCResponse(data []byte, status int, jsonReport bool) error {
	var response rpcResponse
	decodeErr := json.Unmarshal(data, &response)
	if jsonReport {
		if _, err := a.stdout.Write(ensureTrailingNewline(data)); err != nil {
			return markError(kindRender, err)
		}
	}
	if decodeErr != nil {
		if status < 200 || status >= 300 {
			return markError(kindRuntime, fmt.Errorf("server returned HTTP %d: %s", status, singleLine(string(data))))
		}
		if !jsonReport {
			_, err := a.stdout.Write(ensureTrailingNewline(data))
			return markError(kindRender, err)
		}
		return nil
	}
	if response.Error != nil {
		err := markError(rpcClientErrorKind(response.Error), errors.New(response.Error.Message))
		if jsonReport {
			return alreadyReported(err)
		}
		return err
	}
	if status < 200 || status >= 300 {
		return markError(kindRuntime, fmt.Errorf("server returned HTTP %d", status))
	}
	if !jsonReport {
		if response.Result != nil {
			return writeJSON(a.stdout, response.Result)
		}
		return writeJSON(a.stdout, response)
	}
	return nil
}

func rpcClientErrorKind(err *rpcError) errorKind {
	if err == nil {
		return kindRuntime
	}
	switch err.Code {
	case -32602:
		return kindInvalidInput
	case -32800:
		return kindCanceled
	default:
		return kindRuntime
	}
}
