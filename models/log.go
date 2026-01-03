package models

type MediaType string

const (
	MediaText     MediaType = "text"
	MediaPhoto    MediaType = "photo"
	MediaDocument MediaType = "document"
	MediaVideo    MediaType = "video"
)

type LogRequest struct {
	ChatID    		string    `json:"chat_id"`
	Message   		string    `json:"message,omitempty"`
	ParseMode 		string    `json:"parse_mode,omitempty"`
	MediaType 		MediaType `json:"media_type,omitempty"`
	MediaURL  		string    `json:"media_url,omitempty"`
	Caption   		string    `json:"caption,omitempty"`
	MessageThreadID int 	  `json:"message_thread_id,omitempty"`
}

type LogResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	MessageID int    `json:"message_id,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}
