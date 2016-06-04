package main

import (
	"flag"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/gengo/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
	"github.com/gengo/grpc-gateway/protoc-gen-swagger/genswagger"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

var (
	importPrefix = flag.String("import_prefix", "", "prefix to be added to go package paths for imported proto files")
	file         = flag.String("file", "stdin", "where to load data from")
)

func parseReq(r io.Reader) (*plugin.CodeGeneratorRequest, error) {
	glog.V(1).Info("Parsing code generator request")
	input, err := ioutil.ReadAll(r)
	if err != nil {
		glog.Errorf("Failed to read code generator request: %v", err)
		return nil, err
	}
	req := new(plugin.CodeGeneratorRequest)
	if err = proto.Unmarshal(input, req); err != nil {
		glog.Errorf("Failed to unmarshal code generator request: %v", err)
		return nil, err
	}
	glog.V(1).Info("Parsed code generator request")
	return req, nil
}

// Main function of the protobuf compiler (protoc) plugin for generating a
// swagger spec from an appropriately annotated protobuf definition file (a
// file with extention `*.proto`).
//
// In rough terms this is how a protoc plugin works.
//
// How a plugin gets found:
// A user invokes the main `protoc` with some option attached to a particular
// language or action which cooresponds to a protoc plugin with a name that
// matches a binary in a folder in the $PATH. The format for the command-line
// option to name matching is to match the first "word" of the option as split
// into words on underscores, then match that first word to the suffix of some
// executable that starts with 'protoc-gen-*'. What does this mean?
//
// It means that if you invoke the protoc compiler like this:
//
//     protoc   --swagger_out=logtostderr=true:.    example.proto
//
// Because you passed a command line flag that is 'swagger_out', Protoc will
// search in the $PATH for an executable with the name `protoc-gen-swagger`. If
// you invoked protoc with an argument like `--blahblah_out=` then it would
// look for an executable named `protoc-gen-blahblah`.
//
// How does protoc interact with a plugin once it knows which it's looking for?
//
// Once `proto` knows which plugin to use, it will read the indicated `.proto`
// file and parse it into some kind of abstract syntax tree. It then serializes
// it into a byte format (don't know which, may be an ad-hoc format). `protoc`
// then invokes the plugin executable and writes to the stdin of the plugin:
//     1. First it passes the relevant options to the plugins stdin
//     2. It then writes the byte-stream serialized AST to the plugins stdin
//
//
// So, now that the plugin has the AST for the `.proto` file, how does a plugin work?
//
// At this point, some of this is conjecture, but I'm still going to attempt to
// document how I think this works.
// Basically, the plugin will write to it's stdout the results of the
// generation of some alternate language from the AST which it was provided
// from the main `protoc`. It's not clear to me how a plugin could generate
// multiple files, as it seems like there's no mechanism for doing that. But it
// seems possible, since the `protoc-gen-go` plugin generates an entire file
// tree of go code.
// As to HOW EXACTLY an individual plugin does what it does to translate to the
// language it's built to create? That process is totally up to each individual
// plugin. However, all plugins that I know of do leverage the protobuf
// libraries for their language to be able to parse the byte serialized AST
// passed in from the main `protoc`.
func main() {
	flag.Parse()
	defer glog.Flush()

	reg := descriptor.NewRegistry()

	glog.V(1).Info("Processing code generator request")
	f := os.Stdin
	if *file != "stdin" {
		f, _ = os.Open("input.txt")
	}
	req, err := parseReq(f)
	if err != nil {
		glog.Fatal(err)
	}
	if req.Parameter != nil {
		for _, p := range strings.Split(req.GetParameter(), ",") {
			spec := strings.SplitN(p, "=", 2)
			if len(spec) == 1 {
				if err := flag.CommandLine.Set(spec[0], ""); err != nil {
					glog.Fatalf("Cannot set flag %s", p)
				}
				continue
			}
			name, value := spec[0], spec[1]
			if strings.HasPrefix(name, "M") {
				reg.AddPkgMap(name[1:], value)
				continue
			}
			if err := flag.CommandLine.Set(name, value); err != nil {
				glog.Fatalf("Cannot set flag %s", p)
			}
		}
	}

	g := genswagger.New(reg)

	reg.SetPrefix(*importPrefix)
	if err := reg.Load(req); err != nil {
		emitError(err)
		return
	}

	var targets []*descriptor.File
	for _, target := range req.FileToGenerate {
		f, err := reg.LookupFile(target)
		if err != nil {
			glog.Fatal(err)
		}
		targets = append(targets, f)
	}

	out, err := g.Generate(targets)
	glog.V(1).Info("Processed code generator request")
	if err != nil {
		emitError(err)
		return
	}
	emitFiles(out)
}

func emitFiles(out []*plugin.CodeGeneratorResponse_File) {
	emitResp(&plugin.CodeGeneratorResponse{File: out})
}

func emitError(err error) {
	emitResp(&plugin.CodeGeneratorResponse{Error: proto.String(err.Error())})
}

// Write the marshaled output of the provided response to os.Stdout
func emitResp(resp *plugin.CodeGeneratorResponse) {
	buf, err := proto.Marshal(resp)
	if err != nil {
		glog.Fatal(err)
	}
	if _, err := os.Stdout.Write(buf); err != nil {
		glog.Fatal(err)
	}
}
