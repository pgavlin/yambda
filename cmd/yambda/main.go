package main

import (
	_ "encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/goccy/go-yaml/printer"
	"github.com/pgavlin/loom"
	"github.com/pgavlin/yambda"
)

func main() {
	env := loom.NewEnv()

	var v loom.Value
	var err error
	switch len(os.Args) {
	case 1:
		v, err = loom.Parse(os.Stdin)
	default:
		v, err = yambda.EvalFile(env, os.Args[1])
	}
	if err != nil {
		log.Fatal(err)
	}

	f := yambda.MarshalYAMLFile(v, len(os.Args) == 1)

	var p printer.Printer
	for i, doc := range f.Docs {
		if i != 0 {
			fmt.Printf("\n---\n\n")
		}
		fmt.Print(string(p.PrintNode(doc.Body)))
	}

	//	for _, d := range file.Docs {
	//		fmt.Println(string(p.PrintNode(d)))
	//	}
	//
	//
	//	if err = json.NewEncoder(os.Stdout).Encode(v); err != nil {
	//		log.Fatal(err)
	//	}
}
