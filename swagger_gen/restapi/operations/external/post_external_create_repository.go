// Code generated by go-swagger; DO NOT EDIT.

package external

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"context"
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// PostExternalCreateRepositoryHandlerFunc turns a function with the right signature into a post external create repository handler
type PostExternalCreateRepositoryHandlerFunc func(PostExternalCreateRepositoryParams) middleware.Responder

// Handle executing the request and returning a response
func (fn PostExternalCreateRepositoryHandlerFunc) Handle(params PostExternalCreateRepositoryParams) middleware.Responder {
	return fn(params)
}

// PostExternalCreateRepositoryHandler interface for that can handle valid post external create repository params
type PostExternalCreateRepositoryHandler interface {
	Handle(PostExternalCreateRepositoryParams) middleware.Responder
}

// NewPostExternalCreateRepository creates a new http.Handler for the post external create repository operation
func NewPostExternalCreateRepository(ctx *middleware.Context, handler PostExternalCreateRepositoryHandler) *PostExternalCreateRepository {
	return &PostExternalCreateRepository{Context: ctx, Handler: handler}
}

/*
	PostExternalCreateRepository swagger:route POST /external/createrepository external postExternalCreateRepository

Create a Repository via Goliac
*/
type PostExternalCreateRepository struct {
	Context *middleware.Context
	Handler PostExternalCreateRepositoryHandler
}

func (o *PostExternalCreateRepository) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewPostExternalCreateRepositoryParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}

// PostExternalCreateRepositoryBody post external create repository body
//
// swagger:model PostExternalCreateRepositoryBody
type PostExternalCreateRepositoryBody struct {

	// default branch
	// Min Length: 1
	DefaultBranch string `json:"default_branch,omitempty"`

	// github token
	// Required: true
	// Min Length: 40
	GithubToken string `json:"github_token"`

	// repository name
	// Required: true
	// Min Length: 1
	RepositoryName string `json:"repository_name"`

	// team name
	// Required: true
	// Min Length: 1
	TeamName string `json:"team_name"`

	// visibility
	Visibility string `json:"visibility,omitempty"`
}

// Validate validates this post external create repository body
func (o *PostExternalCreateRepositoryBody) Validate(formats strfmt.Registry) error {
	var res []error

	if err := o.validateDefaultBranch(formats); err != nil {
		res = append(res, err)
	}

	if err := o.validateGithubToken(formats); err != nil {
		res = append(res, err)
	}

	if err := o.validateRepositoryName(formats); err != nil {
		res = append(res, err)
	}

	if err := o.validateTeamName(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (o *PostExternalCreateRepositoryBody) validateDefaultBranch(formats strfmt.Registry) error {
	if swag.IsZero(o.DefaultBranch) { // not required
		return nil
	}

	if err := validate.MinLength("body"+"."+"default_branch", "body", o.DefaultBranch, 1); err != nil {
		return err
	}

	return nil
}

func (o *PostExternalCreateRepositoryBody) validateGithubToken(formats strfmt.Registry) error {

	if err := validate.RequiredString("body"+"."+"github_token", "body", o.GithubToken); err != nil {
		return err
	}

	if err := validate.MinLength("body"+"."+"github_token", "body", o.GithubToken, 40); err != nil {
		return err
	}

	return nil
}

func (o *PostExternalCreateRepositoryBody) validateRepositoryName(formats strfmt.Registry) error {

	if err := validate.RequiredString("body"+"."+"repository_name", "body", o.RepositoryName); err != nil {
		return err
	}

	if err := validate.MinLength("body"+"."+"repository_name", "body", o.RepositoryName, 1); err != nil {
		return err
	}

	return nil
}

func (o *PostExternalCreateRepositoryBody) validateTeamName(formats strfmt.Registry) error {

	if err := validate.RequiredString("body"+"."+"team_name", "body", o.TeamName); err != nil {
		return err
	}

	if err := validate.MinLength("body"+"."+"team_name", "body", o.TeamName, 1); err != nil {
		return err
	}

	return nil
}

// ContextValidate validates this post external create repository body based on context it is used
func (o *PostExternalCreateRepositoryBody) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (o *PostExternalCreateRepositoryBody) MarshalBinary() ([]byte, error) {
	if o == nil {
		return nil, nil
	}
	return swag.WriteJSON(o)
}

// UnmarshalBinary interface implementation
func (o *PostExternalCreateRepositoryBody) UnmarshalBinary(b []byte) error {
	var res PostExternalCreateRepositoryBody
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*o = res
	return nil
}
