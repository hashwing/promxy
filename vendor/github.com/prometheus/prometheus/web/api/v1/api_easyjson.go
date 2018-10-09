// Code generated by easyjson for marshaling/unmarshaling. DO NOT EDIT.

package v1

import (
	encodingjson "encoding/json"

	easyjson "github.com/mailru/easyjson"
	jlexer "github.com/mailru/easyjson/jlexer"
	jwriter "github.com/mailru/easyjson/jwriter"
)

// suppress unused package warning
var (
	_ *jlexer.Lexer
	_ *jwriter.Writer
	_ easyjson.Marshaler
)

func easyjsonC1cedd36DecodeGithubComPrometheusPrometheusWebApiV1(in *jlexer.Lexer, out *response) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeString()
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "status":
			out.Status = status(in.String())
		case "data":
			if m, ok := out.Data.(easyjson.Unmarshaler); ok {
				m.UnmarshalEasyJSON(in)
			} else if m, ok := out.Data.(encodingjson.Unmarshaler); ok {
				_ = m.UnmarshalJSON(in.Raw())
			} else {
				out.Data = in.Interface()
			}
		case "errorType":
			out.ErrorType = errorType(in.String())
		case "error":
			out.Error = string(in.String())
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}
func easyjsonC1cedd36EncodeGithubComPrometheusPrometheusWebApiV1(out *jwriter.Writer, in response) {
	out.RawByte('{')
	first := true
	_ = first
	if !first {
		out.RawByte(',')
	}
	first = false
	out.RawString("\"status\":")
	out.String(string(in.Status))
	if in.Data != nil {
		if !first {
			out.RawByte(',')
		}
		first = false
		out.RawString("\"data\":")
		if m, ok := in.Data.(easyjson.Marshaler); ok {
			m.MarshalEasyJSON(out)
		} else if m, ok := in.Data.(encodingjson.Marshaler); ok {
			out.Raw(m.MarshalJSON())
		} else {
			out.Raw(json.Marshal(in.Data))
		}
	}
	if in.ErrorType != "" {
		if !first {
			out.RawByte(',')
		}
		first = false
		out.RawString("\"errorType\":")
		out.String(string(in.ErrorType))
	}
	if in.Error != "" {
		if !first {
			out.RawByte(',')
		}
		first = false
		out.RawString("\"error\":")
		out.String(string(in.Error))
	}
	out.RawByte('}')
}

// MarshalJSON supports json.Marshaler interface
func (v response) MarshalJSON() ([]byte, error) {
	w := jwriter.Writer{}
	easyjsonC1cedd36EncodeGithubComPrometheusPrometheusWebApiV1(&w, v)
	return w.Buffer.BuildBytes(), w.Error
}

// MarshalEasyJSON supports easyjson.Marshaler interface
func (v response) MarshalEasyJSON(w *jwriter.Writer) {
	easyjsonC1cedd36EncodeGithubComPrometheusPrometheusWebApiV1(w, v)
}

// UnmarshalJSON supports json.Unmarshaler interface
func (v *response) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	easyjsonC1cedd36DecodeGithubComPrometheusPrometheusWebApiV1(&r, v)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (v *response) UnmarshalEasyJSON(l *jlexer.Lexer) {
	easyjsonC1cedd36DecodeGithubComPrometheusPrometheusWebApiV1(l, v)
}