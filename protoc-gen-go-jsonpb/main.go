package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

var (
	hdrTmpl = template.Must(template.New("header").Parse(`
// Code generated by protoc-gen-gojsonpb. DO NOT EDIT.
// source: {{.Source}}

package {{.GoPkg}}

import (
    "github.com/golang/protobuf/jsonpb"
)
`))

	jsonpbTmpl = template.Must(template.New("jsonpb").Parse(`
func (msg *{{.GetName}}) MarshalJSON() ([]byte, error) {
    s, err := new(jsonpb.Marshaler).MarshalToString(msg)
    if err != nil {
        return nil, err
    }
    return []byte(s), nil
}

func (msg *{{.GetName}}) UnmarshalJSON(src []byte) error {
    return jsonpb.UnmarshalString(string(src), msg)
}
`))
)

func main() {
	input, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	req := new(plugin.CodeGeneratorRequest)
	if err = proto.Unmarshal(input, req); err != nil {
		log.Fatal(err)
	}

	out, err := generate(req)

	if err != nil {
		emitError(err)
		return
	}

	emitFiles(out)
}

func generate(req *plugin.CodeGeneratorRequest) ([]*plugin.CodeGeneratorResponse_File, error) {
	var files []*plugin.CodeGeneratorResponse_File
	genFileNames := make(map[string]bool)
	for _, n := range req.FileToGenerate {
		genFileNames[n] = true
	}
	for _, desc := range req.GetProtoFile() {
		name := desc.GetName()
		if _, ok := genFileNames[name]; !ok {
			// Only emit output for files present in req.FileToGenerate.
			continue
		}
		code, err := genCode(desc)
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source([]byte(code))
		if err != nil {
			log.Printf("%v: %s", err, code)
			return nil, err
		}

		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		output := fmt.Sprintf("%s.pb.jsonpb.go", base)
		files = append(files, &plugin.CodeGeneratorResponse_File{
			Name:    proto.String(output),
			Content: proto.String(string(formatted)),
		})
	}

	return files, nil
}

func genCode(desc *descriptor.FileDescriptorProto) (string, error) {
	w := bytes.NewBuffer(nil)
	desc.GetOptions()
	hdr := &header{
		Source: desc.GetName(),
		GoPkg:  defaultGoPackageName(desc),
	}

	if err := hdrTmpl.Execute(w, hdr); err != nil {
		log.Fatal(err)
	}

	for _, msgType := range desc.GetMessageType() {
		if err := jsonpbTmpl.Execute(w, msgType); err != nil {
			return "", err
		}
	}

	return w.String(), nil
}

type header struct {
	Source string
	GoPkg  string
}

// sanitizePackageName replaces unallowed character in package name
// with allowed character.
func sanitizePackageName(pkgName string) string {
	pkgName = strings.Replace(pkgName, ".", "_", -1)
	pkgName = strings.Replace(pkgName, "-", "_", -1)
	return pkgName
}

// defaultGoPackageName returns the default go package name to be used for go files generated from "f".
// You might need to use an unique alias for the package when you import it.  Use ReserveGoPackageAlias to get a unique alias.
func defaultGoPackageName(f *descriptor.FileDescriptorProto) string {
	name := packageIdentityName(f)
	return sanitizePackageName(name)
}

// packageIdentityName returns the identity of packages.
// protoc-gen-grpc-gateway rejects CodeGenerationRequests which contains more than one packages
// as protoc-gen-go does.
func packageIdentityName(f *descriptor.FileDescriptorProto) string {
	if f.Options != nil && f.Options.GoPackage != nil {
		gopkg := f.Options.GetGoPackage()
		idx := strings.LastIndex(gopkg, "/")
		if idx < 0 {
			gopkg = gopkg[idx+1:]
		}

		gopkg = gopkg[idx+1:]
		// package name is overrided with the string after the
		// ';' character
		sc := strings.IndexByte(gopkg, ';')
		if sc < 0 {
			return sanitizePackageName(gopkg)

		}
		return sanitizePackageName(gopkg[sc+1:])
	}

	if f.Package == nil {
		base := filepath.Base(f.GetName())
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}
	return f.GetPackage()
}

func emitFiles(out []*plugin.CodeGeneratorResponse_File) {
	emitResp(&plugin.CodeGeneratorResponse{File: out})
}

func emitError(err error) {
	emitResp(&plugin.CodeGeneratorResponse{Error: proto.String(err.Error())})
}

func emitResp(resp *plugin.CodeGeneratorResponse) {
	buf, err := proto.Marshal(resp)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stdout.Write(buf); err != nil {
		log.Fatal(err)
	}
}
