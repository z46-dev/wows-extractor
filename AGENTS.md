## Go Coding Rules

Mandatory coding guidelines for Go:

1. Avoid `:=` as much as possible; use it in loops, select statements, and `if` initializers for comma-ok checks as described in rule 8
2. Use explicit type declarations for variables in blocks
3. Name every function return value, including single-value and error returns. Assign results to the named return variables and use naked `return` statements; do not return expressions directly
4. For all major functions, put a brief one-line comment explaining the function's purpose. If it needs more than a line, expand as needed. Do not over document
5. Put one blank line between distinct logical blocks. Start the function body with a blank line after local declarations; separate guard clauses, validation steps, and subsequent work with blank lines; put a blank line before a final naked `return` when it follows result assignment
6. For checking errors, put the function call into the if statement
7. Use grouped `var (...)` declarations when a block needs more than one local variable. Combine variables of the same type on one declaration line when they share an initialization context, and explicitly initialize grouped variables when values are provided
8. For comma-ok checks, put the assignment in the if statement when the value is only needed for that branch or immediate check
9. In a guard clause or selected branch, assign the error or result to the named return variable and use a naked `return` immediately when later code must not run. When execution should continue, assign the result and fall through to the final naked `return`
10. Optimize for readability and performance always. Conciseness is good, but not at the expense of clarity and performance
11. Declare each named type in its own `type Name ...` declaration. Do not group separate named types into a `type ( ... )` block
12. When setting a value you're going to immediately check, do the assignment in the if statement
13. Inline values used exactly once. Do not create one-use locals solely to name literals, filters, function arguments, or struct values; pass the expression directly where it is consumed. Keep a local only when the value is reused, must survive multi-step error handling, or must remain identical across multiple uses
14. When validating a string against multiple allowed values, prefer a `switch` with the allowed cases grouped and a `default` error branch over chained inequality checks
15. When a composed string uses format placeholders, assign `fmt.Sprintf` to the named string return variable, then use a naked `return`

Here's an example:

```go
var (
    count, other int = 1, 3
    something *reflect.Value
    err error
)

type Something struct {
    Param1, Param2 string
    SomeInt        int
}

type SomethingElse struct {
    Param1, Param2 string
    SomeInt        int
}

// Computes the sum of two integers, returning an error if the result is negative.
func doSomething(a, b int) (c int, err error) {
    if c = a + b; c < 0 {
        err = fmt.Errorf("result is negative")
        c = 0

        // No return here because the return at the end of the function would be literally the same...
    }

    return
}

func main() {
    if count, err = doSomething(1, 2); err != nil {
        fmt.Printf("Error: %v\n", err)
    } else {
        fmt.Printf("Result: %d\n", count)
    }

    something = nil
}
```

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- When graphify-out/graph.json exists, run `graphify query "<question>"` before your first grep, search or source-file read for a codebase question, and put this rule in every subagent prompt that explores code. Read raw files only for lines the graph or an earlier read already located. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
- A committed graphify-out/graph.json is rewritten whole on every build, so a large diff to it in a pull request is expected. Do not flag it as churn or suggest skipping the graph rebuild or its hooks.
