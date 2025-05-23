// Code generated by go-swagger; DO NOT EDIT.

package auth

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/goliac-project/goliac/swagger_gen/models"
)

// GetWorkflowsOKCode is the HTTP code returned for type GetWorkflowsOK
const GetWorkflowsOKCode int = 200

/*
GetWorkflowsOK get list of all PR force merge workflows

swagger:response getWorkflowsOK
*/
type GetWorkflowsOK struct {

	/*
	  In: Body
	*/
	Payload models.Workflows `json:"body,omitempty"`
}

// NewGetWorkflowsOK creates GetWorkflowsOK with default headers values
func NewGetWorkflowsOK() *GetWorkflowsOK {

	return &GetWorkflowsOK{}
}

// WithPayload adds the payload to the get workflows o k response
func (o *GetWorkflowsOK) WithPayload(payload models.Workflows) *GetWorkflowsOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get workflows o k response
func (o *GetWorkflowsOK) SetPayload(payload models.Workflows) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetWorkflowsOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	payload := o.Payload
	if payload == nil {
		// return empty array
		payload = models.Workflows{}
	}

	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// GetWorkflowsUnauthorizedCode is the HTTP code returned for type GetWorkflowsUnauthorized
const GetWorkflowsUnauthorizedCode int = 401

/*
GetWorkflowsUnauthorized Unauthorized

swagger:response getWorkflowsUnauthorized
*/
type GetWorkflowsUnauthorized struct {

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetWorkflowsUnauthorized creates GetWorkflowsUnauthorized with default headers values
func NewGetWorkflowsUnauthorized() *GetWorkflowsUnauthorized {

	return &GetWorkflowsUnauthorized{}
}

// WithPayload adds the payload to the get workflows unauthorized response
func (o *GetWorkflowsUnauthorized) WithPayload(payload *models.Error) *GetWorkflowsUnauthorized {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get workflows unauthorized response
func (o *GetWorkflowsUnauthorized) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetWorkflowsUnauthorized) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(401)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

/*
GetWorkflowsDefault generic error response

swagger:response getWorkflowsDefault
*/
type GetWorkflowsDefault struct {
	_statusCode int

	/*
	  In: Body
	*/
	Payload *models.Error `json:"body,omitempty"`
}

// NewGetWorkflowsDefault creates GetWorkflowsDefault with default headers values
func NewGetWorkflowsDefault(code int) *GetWorkflowsDefault {
	if code <= 0 {
		code = 500
	}

	return &GetWorkflowsDefault{
		_statusCode: code,
	}
}

// WithStatusCode adds the status to the get workflows default response
func (o *GetWorkflowsDefault) WithStatusCode(code int) *GetWorkflowsDefault {
	o._statusCode = code
	return o
}

// SetStatusCode sets the status to the get workflows default response
func (o *GetWorkflowsDefault) SetStatusCode(code int) {
	o._statusCode = code
}

// WithPayload adds the payload to the get workflows default response
func (o *GetWorkflowsDefault) WithPayload(payload *models.Error) *GetWorkflowsDefault {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get workflows default response
func (o *GetWorkflowsDefault) SetPayload(payload *models.Error) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetWorkflowsDefault) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(o._statusCode)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
