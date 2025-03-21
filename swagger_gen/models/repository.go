// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// Repository repository
//
// swagger:model repository
type Repository struct {

	// allow update branch
	AllowUpdateBranch bool `json:"allowUpdateBranch"`

	// archived
	Archived bool `json:"archived"`

	// auto merge allowed
	AutoMergeAllowed bool `json:"autoMergeAllowed"`

	// delete branch on merge
	DeleteBranchOnMerge bool `json:"deleteBranchOnMerge"`

	// name
	Name string `json:"name,omitempty"`

	// visibility
	Visibility string `json:"visibility"`
}

// Validate validates this repository
func (m *Repository) Validate(formats strfmt.Registry) error {
	return nil
}

// ContextValidate validates this repository based on context it is used
func (m *Repository) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *Repository) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *Repository) UnmarshalBinary(b []byte) error {
	var res Repository
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
