package bigflake_test

import (
	"fmt"
	"log"

	"github.com/brunty/bigflake"
)

func ExampleGenerator_Generate() {
	g, err := bigflake.New()
	if err != nil {
		log.Fatal(err)
	}

	id, err := g.Generate("user")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(id.Prefix(), len(id.String()))
	// Output: user 27
}

func ExampleGenerator_Type() {
	g, err := bigflake.New()
	if err != nil {
		log.Fatal(err)
	}

	users, err := g.Type("user")
	if err != nil {
		log.Fatal(err)
	}

	id := users.New() // no error to handle: the prefix was checked once
	fmt.Println(id.Prefix())
	// Output: user
}

func ExampleParse() {
	id, err := bigflake.Parse("user_0000B9JIhkWR2rgYhGjXBQ")
	if err != nil {
		log.Fatal(err)
	}

	parts := id.Decompose()
	fmt.Println(id.Prefix(), parts.Time.Format("2006-01-02"), parts.Sequence)
	// Output: user 2026-08-12 0
}
