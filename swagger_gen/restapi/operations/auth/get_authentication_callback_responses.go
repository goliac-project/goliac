// Code generated by go-swagger; DO NOT EDIT.

package auth

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/goliac-project/goliac/swagger_gen/models"
)

// GetAuthenticationCallbackOKCode is the HTTP code returned for type GetAuthenticationCallbackOK
const GetAuthenticationCallbackOKCode int = 200

/*
GetAuthenticationCallbackOK github user information

swagger:response getAuthenticationCallbackOK
*/
type GetAuthenticationCallbackOK struct {

	/*
	  In: Body
	*/
	Payload *models.Githubuser `json:"body,omitempty"`
}

// NewGetAuthenticationCallbackOK creates GetAuthenticationCallbackOK with default headers values
func NewGetAuthenticationCallbackOK() *GetAuthenticationCallbackOK {

	return &GetAuthenticationCallbackOK{}
}

// WithPayload adds the payload to the get authentication callback o k response
func (o *GetAuthenticationCallbackOK) WithPayload(payload *models.Githubuser) *GetAuthenticationCallbackOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get authentication callback o k response
func (o *GetAuthenticationCallbackOK) SetPayload(payload *models.Githubuser) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetAuthenticationCallbackOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetAuthenticationCallbackFoundCode is the HTTP code returned for type GetAuthenticationCallbackFound
const GetAuthenticationCallbackFoundCode int = 302

/*
GetAuthenticationCallbackFound redirect to github login

swagger:response getAuthenticationCallbackFound
*/
type GetAuthenticationCallbackFound struct {
	/*The URL to redirect to

	 */
	Location strfmt.URI `json:"Location"`
}

// NewGetAuthenticationCallbackFound creates GetAuthenticationCallbackFound with default headers values
func NewGetAuthenticationCallbackFound() *GetAuthenticationCallbackFound {

	return &GetAuthenticationCallbackFound{}
}

// WithLocation adds the location to the get authentication callback found response
func (o *GetAuthenticationCallbackFound) WithLocation(location strfmt.URI) *GetAuthenticationCallbackFound {
	o.Location = location
	return o
}

// SetLocation sets the location to the get authentication callback found response
func (o *GetAuthenticationCallbackFound) SetLocation(location strfmt.URI) {
	o.Location = location
}

// WriteResponse to the client
func (o *GetAuthenticationCallbackFound) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	// response header Location

	location := o.Location.String()
	if location != "" {
		rw.Header().Set("Location", location)
	}

	rw.Header().Del(runtime.HeaderContentType) //Remove Content-Type on empty responses

	rw.WriteHeader(302)
}

/*
GetAuthenticationCallbackDefault generic error response

swagger:response getAuthenticationCallbackDefault
*/
type GetAuthenticationCallbackDefault struct {
	_statusCode int

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetAuthenticationCallbackDefault creates GetAuthenticationCallbackDefault with default headers values
func NewGetAuthenticationCallbackDefault(code int) *GetAuthenticationCallbackDefault {
	if code <= 0 {
		code = 500
	}

	return &GetAuthenticationCallbackDefault{
		_statusCode: code,
	}
}

// WithStatusCode adds the status to the get authentication callback default response
func (o *GetAuthenticationCallbackDefault) WithStatusCode(code int) *GetAuthenticationCallbackDefault {
	o._statusCode = code
	return o
}

// SetStatusCode sets the status to the get authentication callback default response
func (o *GetAuthenticationCallbackDefault) SetStatusCode(code int) {
	o._statusCode = code
}

// WithPayload adds the payload to the get authentication callback default response
func (o *GetAuthenticationCallbackDefault) WithPayload(payload *models.Error) *GetAuthenticationCallbackDefault {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get authentication callback default response
func (o *GetAuthenticationCallbackDefault) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetAuthenticationCallbackDefault) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(o._statusCode)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
