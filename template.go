package main

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/crossplane/function-sdk-go/errors"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	input "github.com/disaster37/function-rancher2/input/v1beta1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TemplateResource templates the input resource with the given request and observed composite resource. The templating is done with the resourcetemplate package, which allows to use Go templates with some additional functions to template the resource. The templating result is then unmarshaled back to the input resource.
func (f *Function) TemplateResource(ctx context.Context, input *input.Input, req *fnv1.RunFunctionRequest) (err error) {
	inputB, err := json.Marshal(input)
	if err != nil {
		return errors.Wrapf(err, "cannot marshal input %T", input)
	}

	tmpl, err := GetNewTemplateWithFunctionMaps(nil).Parse(string(inputB))
	if err != nil {
		return errors.Wrap(err, "cannot parse input as template")
	}

	reqMap, err := convertToMap(req)
	if err != nil {
		return errors.Wrap(err, "cannot convert request to map")
	}

	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, reqMap); err != nil {
		return errors.Wrap(err, "cannot execute prompt template")
	}

	f.log.Debug("Using Resource", "resource", buf.String())

	// recreate resource
	if err = json.Unmarshal(buf.Bytes(), input); err != nil {
		return errors.Wrapf(err, "cannot unmarshal templated resource to %T", input)
	}

	return nil

}

func convertToMap(req *fnv1.RunFunctionRequest) (map[string]any, error) {
	jReq, err := protojson.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal request from proto to json")
	}

	var mReq map[string]any
	if err := json.Unmarshal(jReq, &mReq); err != nil {
		return nil, errors.Wrap(err, "cannot unmarshal json to map[string]any")
	}

	_, ok := mReq["extraResources"]
	if !ok {
		r, ok := mReq["requiredResources"]
		if ok {
			mReq["extraResources"] = r
		}
	}

	return mReq, nil
}
