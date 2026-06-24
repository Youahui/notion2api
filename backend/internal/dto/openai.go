package dto

type ChatMessage struct {
	Role         string `json:"role"`
	Content      any    `json:"content"`
	Name         string `json:"name,omitempty"`
	ToolCallID   string `json:"tool_call_id,omitempty"`
	ToolCalls    any    `json:"tool_calls,omitempty"`
	FunctionCall any    `json:"function_call,omitempty"`
}

type ChatCompletionRequest struct {
	Model              string        `json:"model"`
	Messages           []ChatMessage `json:"messages"`
	Stream             bool          `json:"stream"`
	ConversationID     string        `json:"conversation_id"`
	Conversation       string        `json:"conversation"`
	PromptCacheKey     string        `json:"prompt_cache_key"`
	ThreadID           string        `json:"thread_id"`
	Thread             string        `json:"thread"`
	NotionThreadID     string        `json:"notion_thread_id"`
	User               string        `json:"user"`
	AccountEmail       string        `json:"account_email"`
	NotionAccountEmail string        `json:"notion_account_email"`
	UseWebSearch       *bool         `json:"use_web_search"`
	ResponseFormat     any           `json:"response_format"`
	Metadata           any           `json:"metadata"`
	Tools              any           `json:"tools"`
	ToolChoice         any           `json:"tool_choice"`
	ParallelToolCalls  *bool         `json:"parallel_tool_calls"`
	Attachments        any           `json:"attachments"`
}

type UsageDTO struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	Estimated        bool  `json:"estimated"`
}
