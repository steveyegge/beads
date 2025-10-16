// File generated from our OpenAPI spec by Stainless. See CONTRIBUTING.md for details.

package constant

import (
	shimjson "github.com/anthropics/anthropic-sdk-go/internal/encoding/json"
)

// ModelNonStreamingTokens defines the maximum tokens for models that should limit
// non-streaming requests.
var ModelNonStreamingTokens = map[string]int{
	"claude-opus-4-20250514":                  8192,
	"claude-4-opus-20250514":                  8192,
	"claude-opus-4-0":                         8192,
	"anthropic.claude-opus-4-20250514-v1:0":   8192,
	"claude-opus-4@20250514":                  8192,
	"claude-opus-4-1-20250805":                8192,
	"anthropic.claude-opus-4-1-20250805-v1:0": 8192,
	"claude-opus-4-1@20250805":                8192,
}

type Constant[T any] interface {
	Default() T
}

// ValueOf gives the default value of a constant from its type. It's helpful when
// constructing constants as variants in a one-of. Note that empty structs are
// marshalled by default. Usage: constant.ValueOf[constant.Foo]()
func ValueOf[T Constant[T]]() T {
	var t T
	return t.Default()
}

type Any string                                     // Always "any"
type APIError string                                // Always "api_error"
type ApplicationPDF string                          // Always "application/pdf"
type Approximate string                             // Always "approximate"
type Assistant string                               // Always "assistant"
type AuthenticationError string                     // Always "authentication_error"
type Auto string                                    // Always "auto"
type Base64 string                                  // Always "base64"
type Bash string                                    // Always "bash"
type Bash20241022 string                            // Always "bash_20241022"
type Bash20250124 string                            // Always "bash_20250124"
type BashCodeExecutionOutput string                 // Always "bash_code_execution_output"
type BashCodeExecutionResult string                 // Always "bash_code_execution_result"
type BashCodeExecutionToolResult string             // Always "bash_code_execution_tool_result"
type BashCodeExecutionToolResultError string        // Always "bash_code_execution_tool_result_error"
type BillingError string                            // Always "billing_error"
type Canceled string                                // Always "canceled"
type CharLocation string                            // Always "char_location"
type CitationsDelta string                          // Always "citations_delta"
type ClearToolUses20250919 string                   // Always "clear_tool_uses_20250919"
type CodeExecution string                           // Always "code_execution"
type CodeExecution20250522 string                   // Always "code_execution_20250522"
type CodeExecution20250825 string                   // Always "code_execution_20250825"
type CodeExecutionOutput string                     // Always "code_execution_output"
type CodeExecutionResult string                     // Always "code_execution_result"
type CodeExecutionToolResult string                 // Always "code_execution_tool_result"
type CodeExecutionToolResultError string            // Always "code_execution_tool_result_error"
type Completion string                              // Always "completion"
type Computer string                                // Always "computer"
type Computer20241022 string                        // Always "computer_20241022"
type Computer20250124 string                        // Always "computer_20250124"
type ContainerUpload string                         // Always "container_upload"
type Content string                                 // Always "content"
type ContentBlockDelta string                       // Always "content_block_delta"
type ContentBlockLocation string                    // Always "content_block_location"
type ContentBlockStart string                       // Always "content_block_start"
type ContentBlockStop string                        // Always "content_block_stop"
type Create string                                  // Always "create"
type Delete string                                  // Always "delete"
type Disabled string                                // Always "disabled"
type Document string                                // Always "document"
type Enabled string                                 // Always "enabled"
type Ephemeral string                               // Always "ephemeral"
type Error string                                   // Always "error"
type Errored string                                 // Always "errored"
type Expired string                                 // Always "expired"
type File string                                    // Always "file"
type Image string                                   // Always "image"
type InputJSONDelta string                          // Always "input_json_delta"
type InputTokens string                             // Always "input_tokens"
type Insert string                                  // Always "insert"
type InvalidRequestError string                     // Always "invalid_request_error"
type MCPToolResult string                           // Always "mcp_tool_result"
type MCPToolUse string                              // Always "mcp_tool_use"
type Memory string                                  // Always "memory"
type Memory20250818 string                          // Always "memory_20250818"
type Message string                                 // Always "message"
type MessageBatch string                            // Always "message_batch"
type MessageBatchDeleted string                     // Always "message_batch_deleted"
type MessageDelta string                            // Always "message_delta"
type MessageStart string                            // Always "message_start"
type MessageStop string                             // Always "message_stop"
type Model string                                   // Always "model"
type None string                                    // Always "none"
type NotFoundError string                           // Always "not_found_error"
type Object string                                  // Always "object"
type OverloadedError string                         // Always "overloaded_error"
type PageLocation string                            // Always "page_location"
type PermissionError string                         // Always "permission_error"
type RateLimitError string                          // Always "rate_limit_error"
type RedactedThinking string                        // Always "redacted_thinking"
type Rename string                                  // Always "rename"
type SearchResult string                            // Always "search_result"
type SearchResultLocation string                    // Always "search_result_location"
type ServerToolUse string                           // Always "server_tool_use"
type SignatureDelta string                          // Always "signature_delta"
type StrReplace string                              // Always "str_replace"
type StrReplaceBasedEditTool string                 // Always "str_replace_based_edit_tool"
type StrReplaceEditor string                        // Always "str_replace_editor"
type Succeeded string                               // Always "succeeded"
type Text string                                    // Always "text"
type TextDelta string                               // Always "text_delta"
type TextEditor20241022 string                      // Always "text_editor_20241022"
type TextEditor20250124 string                      // Always "text_editor_20250124"
type TextEditor20250429 string                      // Always "text_editor_20250429"
type TextEditor20250728 string                      // Always "text_editor_20250728"
type TextEditorCodeExecutionCreateResult string     // Always "text_editor_code_execution_create_result"
type TextEditorCodeExecutionStrReplaceResult string // Always "text_editor_code_execution_str_replace_result"
type TextEditorCodeExecutionToolResult string       // Always "text_editor_code_execution_tool_result"
type TextEditorCodeExecutionToolResultError string  // Always "text_editor_code_execution_tool_result_error"
type TextEditorCodeExecutionViewResult string       // Always "text_editor_code_execution_view_result"
type TextPlain string                               // Always "text/plain"
type Thinking string                                // Always "thinking"
type ThinkingDelta string                           // Always "thinking_delta"
type TimeoutError string                            // Always "timeout_error"
type Tool string                                    // Always "tool"
type ToolResult string                              // Always "tool_result"
type ToolUse string                                 // Always "tool_use"
type ToolUses string                                // Always "tool_uses"
type URL string                                     // Always "url"
type View string                                    // Always "view"
type WebFetch string                                // Always "web_fetch"
type WebFetch20250910 string                        // Always "web_fetch_20250910"
type WebFetchResult string                          // Always "web_fetch_result"
type WebFetchToolResult string                      // Always "web_fetch_tool_result"
type WebFetchToolResultError string                 // Always "web_fetch_tool_result_error"
type WebSearch string                               // Always "web_search"
type WebSearch20250305 string                       // Always "web_search_20250305"
type WebSearchResult string                         // Always "web_search_result"
type WebSearchResultLocation string                 // Always "web_search_result_location"
type WebSearchToolResult string                     // Always "web_search_tool_result"
type WebSearchToolResultError string                // Always "web_search_tool_result_error"

func (c Any) Default() Any                                 { return "any" }
func (c APIError) Default() APIError                       { return "api_error" }
func (c ApplicationPDF) Default() ApplicationPDF           { return "application/pdf" }
func (c Approximate) Default() Approximate                 { return "approximate" }
func (c Assistant) Default() Assistant                     { return "assistant" }
func (c AuthenticationError) Default() AuthenticationError { return "authentication_error" }
func (c Auto) Default() Auto                               { return "auto" }
func (c Base64) Default() Base64                           { return "base64" }
func (c Bash) Default() Bash                               { return "bash" }
func (c Bash20241022) Default() Bash20241022               { return "bash_20241022" }
func (c Bash20250124) Default() Bash20250124               { return "bash_20250124" }
func (c BashCodeExecutionOutput) Default() BashCodeExecutionOutput {
	return "bash_code_execution_output"
}
func (c BashCodeExecutionResult) Default() BashCodeExecutionResult {
	return "bash_code_execution_result"
}
func (c BashCodeExecutionToolResult) Default() BashCodeExecutionToolResult {
	return "bash_code_execution_tool_result"
}
func (c BashCodeExecutionToolResultError) Default() BashCodeExecutionToolResultError {
	return "bash_code_execution_tool_result_error"
}
func (c BillingError) Default() BillingError                   { return "billing_error" }
func (c Canceled) Default() Canceled                           { return "canceled" }
func (c CharLocation) Default() CharLocation                   { return "char_location" }
func (c CitationsDelta) Default() CitationsDelta               { return "citations_delta" }
func (c ClearToolUses20250919) Default() ClearToolUses20250919 { return "clear_tool_uses_20250919" }
func (c CodeExecution) Default() CodeExecution                 { return "code_execution" }
func (c CodeExecution20250522) Default() CodeExecution20250522 { return "code_execution_20250522" }
func (c CodeExecution20250825) Default() CodeExecution20250825 { return "code_execution_20250825" }
func (c CodeExecutionOutput) Default() CodeExecutionOutput     { return "code_execution_output" }
func (c CodeExecutionResult) Default() CodeExecutionResult     { return "code_execution_result" }
func (c CodeExecutionToolResult) Default() CodeExecutionToolResult {
	return "code_execution_tool_result"
}
func (c CodeExecutionToolResultError) Default() CodeExecutionToolResultError {
	return "code_execution_tool_result_error"
}
func (c Completion) Default() Completion                     { return "completion" }
func (c Computer) Default() Computer                         { return "computer" }
func (c Computer20241022) Default() Computer20241022         { return "computer_20241022" }
func (c Computer20250124) Default() Computer20250124         { return "computer_20250124" }
func (c ContainerUpload) Default() ContainerUpload           { return "container_upload" }
func (c Content) Default() Content                           { return "content" }
func (c ContentBlockDelta) Default() ContentBlockDelta       { return "content_block_delta" }
func (c ContentBlockLocation) Default() ContentBlockLocation { return "content_block_location" }
func (c ContentBlockStart) Default() ContentBlockStart       { return "content_block_start" }
func (c ContentBlockStop) Default() ContentBlockStop         { return "content_block_stop" }
func (c Create) Default() Create                             { return "create" }
func (c Delete) Default() Delete                             { return "delete" }
func (c Disabled) Default() Disabled                         { return "disabled" }
func (c Document) Default() Document                         { return "document" }
func (c Enabled) Default() Enabled                           { return "enabled" }
func (c Ephemeral) Default() Ephemeral                       { return "ephemeral" }
func (c Error) Default() Error                               { return "error" }
func (c Errored) Default() Errored                           { return "errored" }
func (c Expired) Default() Expired                           { return "expired" }
func (c File) Default() File                                 { return "file" }
func (c Image) Default() Image                               { return "image" }
func (c InputJSONDelta) Default() InputJSONDelta             { return "input_json_delta" }
func (c InputTokens) Default() InputTokens                   { return "input_tokens" }
func (c Insert) Default() Insert                             { return "insert" }
func (c InvalidRequestError) Default() InvalidRequestError   { return "invalid_request_error" }
func (c MCPToolResult) Default() MCPToolResult               { return "mcp_tool_result" }
func (c MCPToolUse) Default() MCPToolUse                     { return "mcp_tool_use" }
func (c Memory) Default() Memory                             { return "memory" }
func (c Memory20250818) Default() Memory20250818             { return "memory_20250818" }
func (c Message) Default() Message                           { return "message" }
func (c MessageBatch) Default() MessageBatch                 { return "message_batch" }
func (c MessageBatchDeleted) Default() MessageBatchDeleted   { return "message_batch_deleted" }
func (c MessageDelta) Default() MessageDelta                 { return "message_delta" }
func (c MessageStart) Default() MessageStart                 { return "message_start" }
func (c MessageStop) Default() MessageStop                   { return "message_stop" }
func (c Model) Default() Model                               { return "model" }
func (c None) Default() None                                 { return "none" }
func (c NotFoundError) Default() NotFoundError               { return "not_found_error" }
func (c Object) Default() Object                             { return "object" }
func (c OverloadedError) Default() OverloadedError           { return "overloaded_error" }
func (c PageLocation) Default() PageLocation                 { return "page_location" }
func (c PermissionError) Default() PermissionError           { return "permission_error" }
func (c RateLimitError) Default() RateLimitError             { return "rate_limit_error" }
func (c RedactedThinking) Default() RedactedThinking         { return "redacted_thinking" }
func (c Rename) Default() Rename                             { return "rename" }
func (c SearchResult) Default() SearchResult                 { return "search_result" }
func (c SearchResultLocation) Default() SearchResultLocation { return "search_result_location" }
func (c ServerToolUse) Default() ServerToolUse               { return "server_tool_use" }
func (c SignatureDelta) Default() SignatureDelta             { return "signature_delta" }
func (c StrReplace) Default() StrReplace                     { return "str_replace" }
func (c StrReplaceBasedEditTool) Default() StrReplaceBasedEditTool {
	return "str_replace_based_edit_tool"
}
func (c StrReplaceEditor) Default() StrReplaceEditor     { return "str_replace_editor" }
func (c Succeeded) Default() Succeeded                   { return "succeeded" }
func (c Text) Default() Text                             { return "text" }
func (c TextDelta) Default() TextDelta                   { return "text_delta" }
func (c TextEditor20241022) Default() TextEditor20241022 { return "text_editor_20241022" }
func (c TextEditor20250124) Default() TextEditor20250124 { return "text_editor_20250124" }
func (c TextEditor20250429) Default() TextEditor20250429 { return "text_editor_20250429" }
func (c TextEditor20250728) Default() TextEditor20250728 { return "text_editor_20250728" }
func (c TextEditorCodeExecutionCreateResult) Default() TextEditorCodeExecutionCreateResult {
	return "text_editor_code_execution_create_result"
}
func (c TextEditorCodeExecutionStrReplaceResult) Default() TextEditorCodeExecutionStrReplaceResult {
	return "text_editor_code_execution_str_replace_result"
}
func (c TextEditorCodeExecutionToolResult) Default() TextEditorCodeExecutionToolResult {
	return "text_editor_code_execution_tool_result"
}
func (c TextEditorCodeExecutionToolResultError) Default() TextEditorCodeExecutionToolResultError {
	return "text_editor_code_execution_tool_result_error"
}
func (c TextEditorCodeExecutionViewResult) Default() TextEditorCodeExecutionViewResult {
	return "text_editor_code_execution_view_result"
}
func (c TextPlain) Default() TextPlain                   { return "text/plain" }
func (c Thinking) Default() Thinking                     { return "thinking" }
func (c ThinkingDelta) Default() ThinkingDelta           { return "thinking_delta" }
func (c TimeoutError) Default() TimeoutError             { return "timeout_error" }
func (c Tool) Default() Tool                             { return "tool" }
func (c ToolResult) Default() ToolResult                 { return "tool_result" }
func (c ToolUse) Default() ToolUse                       { return "tool_use" }
func (c ToolUses) Default() ToolUses                     { return "tool_uses" }
func (c URL) Default() URL                               { return "url" }
func (c View) Default() View                             { return "view" }
func (c WebFetch) Default() WebFetch                     { return "web_fetch" }
func (c WebFetch20250910) Default() WebFetch20250910     { return "web_fetch_20250910" }
func (c WebFetchResult) Default() WebFetchResult         { return "web_fetch_result" }
func (c WebFetchToolResult) Default() WebFetchToolResult { return "web_fetch_tool_result" }
func (c WebFetchToolResultError) Default() WebFetchToolResultError {
	return "web_fetch_tool_result_error"
}
func (c WebSearch) Default() WebSearch                 { return "web_search" }
func (c WebSearch20250305) Default() WebSearch20250305 { return "web_search_20250305" }
func (c WebSearchResult) Default() WebSearchResult     { return "web_search_result" }
func (c WebSearchResultLocation) Default() WebSearchResultLocation {
	return "web_search_result_location"
}
func (c WebSearchToolResult) Default() WebSearchToolResult { return "web_search_tool_result" }
func (c WebSearchToolResultError) Default() WebSearchToolResultError {
	return "web_search_tool_result_error"
}

func (c Any) MarshalJSON() ([]byte, error)                                 { return marshalString(c) }
func (c APIError) MarshalJSON() ([]byte, error)                            { return marshalString(c) }
func (c ApplicationPDF) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c Approximate) MarshalJSON() ([]byte, error)                         { return marshalString(c) }
func (c Assistant) MarshalJSON() ([]byte, error)                           { return marshalString(c) }
func (c AuthenticationError) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c Auto) MarshalJSON() ([]byte, error)                                { return marshalString(c) }
func (c Base64) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c Bash) MarshalJSON() ([]byte, error)                                { return marshalString(c) }
func (c Bash20241022) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c Bash20250124) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c BashCodeExecutionOutput) MarshalJSON() ([]byte, error)             { return marshalString(c) }
func (c BashCodeExecutionResult) MarshalJSON() ([]byte, error)             { return marshalString(c) }
func (c BashCodeExecutionToolResult) MarshalJSON() ([]byte, error)         { return marshalString(c) }
func (c BashCodeExecutionToolResultError) MarshalJSON() ([]byte, error)    { return marshalString(c) }
func (c BillingError) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c Canceled) MarshalJSON() ([]byte, error)                            { return marshalString(c) }
func (c CharLocation) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c CitationsDelta) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c ClearToolUses20250919) MarshalJSON() ([]byte, error)               { return marshalString(c) }
func (c CodeExecution) MarshalJSON() ([]byte, error)                       { return marshalString(c) }
func (c CodeExecution20250522) MarshalJSON() ([]byte, error)               { return marshalString(c) }
func (c CodeExecution20250825) MarshalJSON() ([]byte, error)               { return marshalString(c) }
func (c CodeExecutionOutput) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c CodeExecutionResult) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c CodeExecutionToolResult) MarshalJSON() ([]byte, error)             { return marshalString(c) }
func (c CodeExecutionToolResultError) MarshalJSON() ([]byte, error)        { return marshalString(c) }
func (c Completion) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c Computer) MarshalJSON() ([]byte, error)                            { return marshalString(c) }
func (c Computer20241022) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c Computer20250124) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c ContainerUpload) MarshalJSON() ([]byte, error)                     { return marshalString(c) }
func (c Content) MarshalJSON() ([]byte, error)                             { return marshalString(c) }
func (c ContentBlockDelta) MarshalJSON() ([]byte, error)                   { return marshalString(c) }
func (c ContentBlockLocation) MarshalJSON() ([]byte, error)                { return marshalString(c) }
func (c ContentBlockStart) MarshalJSON() ([]byte, error)                   { return marshalString(c) }
func (c ContentBlockStop) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c Create) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c Delete) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c Disabled) MarshalJSON() ([]byte, error)                            { return marshalString(c) }
func (c Document) MarshalJSON() ([]byte, error)                            { return marshalString(c) }
func (c Enabled) MarshalJSON() ([]byte, error)                             { return marshalString(c) }
func (c Ephemeral) MarshalJSON() ([]byte, error)                           { return marshalString(c) }
func (c Error) MarshalJSON() ([]byte, error)                               { return marshalString(c) }
func (c Errored) MarshalJSON() ([]byte, error)                             { return marshalString(c) }
func (c Expired) MarshalJSON() ([]byte, error)                             { return marshalString(c) }
func (c File) MarshalJSON() ([]byte, error)                                { return marshalString(c) }
func (c Image) MarshalJSON() ([]byte, error)                               { return marshalString(c) }
func (c InputJSONDelta) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c InputTokens) MarshalJSON() ([]byte, error)                         { return marshalString(c) }
func (c Insert) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c InvalidRequestError) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c MCPToolResult) MarshalJSON() ([]byte, error)                       { return marshalString(c) }
func (c MCPToolUse) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c Memory) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c Memory20250818) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c Message) MarshalJSON() ([]byte, error)                             { return marshalString(c) }
func (c MessageBatch) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c MessageBatchDeleted) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c MessageDelta) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c MessageStart) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c MessageStop) MarshalJSON() ([]byte, error)                         { return marshalString(c) }
func (c Model) MarshalJSON() ([]byte, error)                               { return marshalString(c) }
func (c None) MarshalJSON() ([]byte, error)                                { return marshalString(c) }
func (c NotFoundError) MarshalJSON() ([]byte, error)                       { return marshalString(c) }
func (c Object) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c OverloadedError) MarshalJSON() ([]byte, error)                     { return marshalString(c) }
func (c PageLocation) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c PermissionError) MarshalJSON() ([]byte, error)                     { return marshalString(c) }
func (c RateLimitError) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c RedactedThinking) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c Rename) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c SearchResult) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c SearchResultLocation) MarshalJSON() ([]byte, error)                { return marshalString(c) }
func (c ServerToolUse) MarshalJSON() ([]byte, error)                       { return marshalString(c) }
func (c SignatureDelta) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c StrReplace) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c StrReplaceBasedEditTool) MarshalJSON() ([]byte, error)             { return marshalString(c) }
func (c StrReplaceEditor) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c Succeeded) MarshalJSON() ([]byte, error)                           { return marshalString(c) }
func (c Text) MarshalJSON() ([]byte, error)                                { return marshalString(c) }
func (c TextDelta) MarshalJSON() ([]byte, error)                           { return marshalString(c) }
func (c TextEditor20241022) MarshalJSON() ([]byte, error)                  { return marshalString(c) }
func (c TextEditor20250124) MarshalJSON() ([]byte, error)                  { return marshalString(c) }
func (c TextEditor20250429) MarshalJSON() ([]byte, error)                  { return marshalString(c) }
func (c TextEditor20250728) MarshalJSON() ([]byte, error)                  { return marshalString(c) }
func (c TextEditorCodeExecutionCreateResult) MarshalJSON() ([]byte, error) { return marshalString(c) }
func (c TextEditorCodeExecutionStrReplaceResult) MarshalJSON() ([]byte, error) {
	return marshalString(c)
}
func (c TextEditorCodeExecutionToolResult) MarshalJSON() ([]byte, error) { return marshalString(c) }
func (c TextEditorCodeExecutionToolResultError) MarshalJSON() ([]byte, error) {
	return marshalString(c)
}
func (c TextEditorCodeExecutionViewResult) MarshalJSON() ([]byte, error) { return marshalString(c) }
func (c TextPlain) MarshalJSON() ([]byte, error)                         { return marshalString(c) }
func (c Thinking) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c ThinkingDelta) MarshalJSON() ([]byte, error)                     { return marshalString(c) }
func (c TimeoutError) MarshalJSON() ([]byte, error)                      { return marshalString(c) }
func (c Tool) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c ToolResult) MarshalJSON() ([]byte, error)                        { return marshalString(c) }
func (c ToolUse) MarshalJSON() ([]byte, error)                           { return marshalString(c) }
func (c ToolUses) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c URL) MarshalJSON() ([]byte, error)                               { return marshalString(c) }
func (c View) MarshalJSON() ([]byte, error)                              { return marshalString(c) }
func (c WebFetch) MarshalJSON() ([]byte, error)                          { return marshalString(c) }
func (c WebFetch20250910) MarshalJSON() ([]byte, error)                  { return marshalString(c) }
func (c WebFetchResult) MarshalJSON() ([]byte, error)                    { return marshalString(c) }
func (c WebFetchToolResult) MarshalJSON() ([]byte, error)                { return marshalString(c) }
func (c WebFetchToolResultError) MarshalJSON() ([]byte, error)           { return marshalString(c) }
func (c WebSearch) MarshalJSON() ([]byte, error)                         { return marshalString(c) }
func (c WebSearch20250305) MarshalJSON() ([]byte, error)                 { return marshalString(c) }
func (c WebSearchResult) MarshalJSON() ([]byte, error)                   { return marshalString(c) }
func (c WebSearchResultLocation) MarshalJSON() ([]byte, error)           { return marshalString(c) }
func (c WebSearchToolResult) MarshalJSON() ([]byte, error)               { return marshalString(c) }
func (c WebSearchToolResultError) MarshalJSON() ([]byte, error)          { return marshalString(c) }

type constant[T any] interface {
	Constant[T]
	*T
}

func marshalString[T ~string, PT constant[T]](v T) ([]byte, error) {
	var zero T
	if v == zero {
		v = PT(&v).Default()
	}
	return shimjson.Marshal(string(v))
}
