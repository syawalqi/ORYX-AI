     1|package llm
     2|
     3|import "context"
     4|
     5|type Role string
     6|
     7|const (
     8|	RoleSystem    Role = "system"
     9|	RoleUser      Role = "user"
    10|	RoleAssistant Role = "assistant"
    11|	RoleTool      Role = "tool"
    12|)
    13|
    14|type Message struct {
    15|	Role       Role       `json:"role"`
    16|	Content    string     `json:"content"`
    17|	ToolCallID string     `json:"tool_call_id,omitempty"`
    18|	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    19|}
    20|
    21|type ToolCall struct {
    22|	ID       string       `json:"id"`
    23|	Type     string       `json:"type"`
    24|	Function ToolFunction `json:"function"`
    25|}
    26|
    27|type ToolFunction struct {
    28|	Name      string `json:"name"`
    29|	Arguments string `json:"arguments"`
    30|}
    31|