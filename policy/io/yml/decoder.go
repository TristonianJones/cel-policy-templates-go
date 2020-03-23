// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yml

import (
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/google/cel-go/common"
	"github.com/google/cel-policy-templates-go/policy/model"
)

func DecodeInstance(src *model.Source) (*model.Instance, *common.Errors) {
	errs := common.NewErrors(src)
	var docNode yaml.Node
	err := yaml.Unmarshal([]byte(src.Content()), &docNode)
	if err != nil {
		errs.ReportError(common.NoLocation, err.Error())
		return nil, errs
	}
	if docNode.Kind != yaml.DocumentNode {
		errs.ReportError(common.NoLocation,
			"got yaml node of kind %v, wanted mapping node",
			docNode.Kind)
		return nil, errs
	}
	info := model.NewSourceInfo(src)
	inst := &model.Instance{SourceInfo: info}
	ib := newInstanceBuilder(inst)
	dec := &decoder{
		info: info,
		errs: errs,
	}
	dec.collectMetadata(1, &docNode)
	dec.decode(docNode.Content[0], ib)
	return inst, errs
}

type decoder struct {
	id   int64
	info *model.SourceInfo
	errs *common.Errors
}

func (d *decoder) nextID() int64 {
	d.id++
	return d.id
}

func (d *decoder) collectMetadata(id int64, node *yaml.Node) {
	var comments []*model.Comment
	if txt := node.HeadComment; txt != "" {
		comments = append(comments, model.NewHeadComment(txt))
	}
	if txt := node.LineComment; txt != "" {
		comments = append(comments, model.NewLineComment(txt))
	}
	if txt := node.FootComment; txt != "" {
		comments = append(comments, model.NewFootComment(txt))
	}
	if len(comments) > 0 {
		d.info.Comments[id] = comments
	}
	offset := int32(0)
	if node.Line > 1 {
		offset = d.info.LineOffsets[node.Line-2]
	}
	d.info.Offsets[id] = offset + int32(node.Column) - 1
}

func (d *decoder) decode(node *yaml.Node, ref objRef) {
	id := d.nextID()
	d.collectMetadata(id, node)
	ref.id(id)
	celType, found := yamlTypes[node.LongTag()]
	if !found {
		d.reportErrorAtID(id, "unsupported yaml type: %s", node.LongTag())
		return
	}
	switch celType {
	case "list":
		d.decodeSeq(node, ref)
	case "map":
		d.decodeMap(node, ref)
	default:
		d.decodePrimitive(node, ref)
	}
}

func (d *decoder) decodePrimitive(node *yaml.Node, ref objRef) {
	var err error
	celType := yamlTypes[node.LongTag()]
	switch celType {
	case "bool":
		ref.assign(node.Value == "true")
	case "double":
		val, convErr := strconv.ParseFloat(node.Value, 64)
		if err != nil {
			d.reportErrorAtID(d.id, convErr.Error())
		}
		err = ref.assign(val)
	case "int":
		val, convErr := strconv.ParseInt(node.Value, 10, 64)
		if err != nil {
			d.reportErrorAtID(d.id, convErr.Error())
		}
		err = ref.assign(val)
	case "null":
		err = ref.assign(model.Null)
	case "string":
		err = ref.assign(node.Value)
	default:
		d.reportErrorAtID(d.id, "unsupported cel type: %s", celType)
	}
	if err != nil {
		d.reportErrorAtID(d.id, err.Error())
	}
}

func (d *decoder) decodeSeq(node *yaml.Node, ref objRef) {
	for i, val := range node.Content {
		elem, err := ref.propAt(i)
		if err != nil {
			d.reportErrorAtID(d.id, err.Error())
		} else {
			d.decode(val, elem)
		}
	}
}

func (d *decoder) decodeMap(node *yaml.Node, ref objRef) {
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		id := d.nextID()
		d.collectMetadata(id, key)

		val := node.Content[i+1]
		keyType, found := yamlTypes[key.LongTag()]
		if !found || keyType != "string" {
			d.reportErrorAtID(id, "invalid map key type: %v", key.LongTag())
			continue
		}
		prop := key.Value
		propRef, err := ref.prop(id, prop)
		if err != nil {
			d.reportErrorAtID(id, err.Error())
			continue
		}
		d.decode(val, propRef)
	}
}

func (d *decoder) reportErrorAtID(id int64, format string, args ...interface{}) {
	loc, found := d.info.LocationByID(id)
	if !found {
		loc = common.NoLocation
	}
	d.errs.ReportError(loc, format, args...)
}

var (
	openAPISchemaTypes map[string]string = map[string]string{
		"boolean": "bool",
		"null":    "null",
		"string":  "string",
		"integer": "int",
		"number":  "double",
		"array":   "list",
		"object":  "map",
		"dyn":     "dyn",
	}
	yamlTypes = map[string]string{
		"tag:yaml.org,2002:bool":  "bool",
		"tag:yaml.org,2002:null":  "null",
		"tag:yaml.org,2002:str":   "string",
		"tag:yaml.org,2002:int":   "int",
		"tag:yaml.org,2002:float": "double",
		"tag:yaml.org,2002:seq":   "list",
		"tag:yaml.org,2002:map":   "map",
	}
)
