// Code generated by go-swagger; DO NOT EDIT.

package app

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/goliac-project/goliac/swagger_gen/models"
)

// GetCollaboratorOKCode is the HTTP code returned for type GetCollaboratorOK
const GetCollaboratorOKCode int = 200

/*
GetCollaboratorOK get collaborator details especially repositories

swagger:response getCollaboratorOK
*/
type GetCollaboratorOK struct {

	/*
	  In: Body
	*/
	Payload *models.CollaboratorDetails `json:"body,omitempty"`
}

// NewGetCollaboratorOK creates GetCollaboratorOK with default headers values
func NewGetCollaboratorOK() *GetCollaboratorOK {

	return &GetCollaboratorOK{}
}

// WithPayload adds the payload to the get collaborator o k response
func (o *GetCollaboratorOK) WithPayload(payload *models.CollaboratorDetails) *GetCollaboratorOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get collaborator o k response
func (o *GetCollaboratorOK) SetPayload(payload *models.CollaboratorDetails) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetCollaboratorOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

/*
GetCollaboratorDefault generic error response

swagger:response getCollaboratorDefault
*/
type GetCollaboratorDefault struct {
	_statusCode int

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetCollaboratorDefault creates GetCollaboratorDefault with default headers values
func NewGetCollaboratorDefault(code int) *GetCollaboratorDefault {
	if code <= 0 {
		code = 500
	}

	return &GetCollaboratorDefault{
		_statusCode: code,
	}
}

// WithStatusCode adds the status to the get collaborator default response
func (o *GetCollaboratorDefault) WithStatusCode(code int) *GetCollaboratorDefault {
	o._statusCode = code
	return o
}

// SetStatusCode sets the status to the get collaborator default response
func (o *GetCollaboratorDefault) SetStatusCode(code int) {
	o._statusCode = code
}

// WithPayload adds the payload to the get collaborator default response
func (o *GetCollaboratorDefault) WithPayload(payload *models.Error) *GetCollaboratorDefault {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get collaborator default response
func (o *GetCollaboratorDefault) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetCollaboratorDefault) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(o._statusCode)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
