/**
 * Copyright (C) 2011 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *         http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package schemagen

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

type PackageDescriptor struct {
	GoPackage   string
	JavaPackage string
	Prefix      string
}

type schemaGenerator struct {
	types    map[reflect.Type]*JSONObjectDescriptor
	packages map[string]PackageDescriptor
	typeMap  map[reflect.Type]reflect.Type
}

func GenerateSchema(t reflect.Type, packages []PackageDescriptor, typeMap map[reflect.Type]reflect.Type) (*JSONSchema, error) {
	g := newSchemaGenerator(packages, typeMap)
	return g.generate(t)
}

func newSchemaGenerator(packages []PackageDescriptor, typeMap map[reflect.Type]reflect.Type) *schemaGenerator {
	pkgMap := make(map[string]PackageDescriptor)
	for _, p := range packages {
		pkgMap[p.GoPackage] = p
	}
	g := schemaGenerator{
		types:    make(map[reflect.Type]*JSONObjectDescriptor),
		packages: pkgMap,
		typeMap:  typeMap,
	}
	return &g
}

func getFieldName(f reflect.StructField) string {
	json := f.Tag.Get("json")
	if len(json) > 0 {
		parts := strings.Split(json, ",")
		return parts[0]
	}
	return f.Name
}

func getFieldDescription(f reflect.StructField) string {
	json := f.Tag.Get("description")
	if len(json) > 0 {
		parts := strings.Split(json, ",")
		return parts[0]
	}
	return ""
}

func (g *schemaGenerator) qualifiedName(t reflect.Type) string {
	pkgDesc, ok := g.packages[pkgPath(t)]
	if !ok {
		prefix := strings.Replace(pkgPath(t), "/", "_", -1)
		prefix = strings.Replace(prefix, ".", "_", -1)
		prefix = strings.Replace(prefix, "-", "_", -1)
		return prefix + "_" + t.Name()
	} else {
		return pkgDesc.Prefix + t.Name()
	}
}

func (g *schemaGenerator) resourceDetails(t reflect.Type) string {
	var name = strings.ToLower(t.Name())
	return name
}

func (g *schemaGenerator) generateReference(t reflect.Type) string {
	return "#/definitions/" + g.qualifiedName(t)
}

func (g *schemaGenerator) javaTypeArrayList(t reflect.Type) string {
	typeName := g.javaTypeWrapPrimitive(t)
	switch typeName {
	case "Byte", "Integer":
		return "String"
	default:
		return "java.util.ArrayList<" + typeName + ">"
	}
}

func (g *schemaGenerator) javaTypeWrapPrimitive(t reflect.Type) string {
	typeName := g.javaType(t)
	switch typeName {
	case "bool":
		return "Boolean"
	case "char":
		return "Character"
	case "short":
		return "Short"
	case "int":
		return "Integer"
	case "long":
		return "Long"
	case "float":
		return "Float"
	case "double":
		return "Double"
	default:
		return typeName
	}
}

func (g *schemaGenerator) javaType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.Name()
	path := pkgPath(t)
	pkgDesc, ok := g.packages[path]
	if t.Kind() == reflect.Struct && ok {
		switch name {
		case "Time":
			return "String"
		case "RawExtension":
			return "io.fabric8.kubernetes.api.model.HasMetadata"
		case "List":
			return pkgDesc.JavaPackage + ".BaseKubernetesList"
		default:
			return pkgDesc.JavaPackage + "." + name
		}
	} else {
		switch t.Kind() {
		case reflect.Bool:
			return "bool"
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Uint,
			reflect.Uint8, reflect.Uint16, reflect.Uint32:
			return "int"
		case reflect.Int64, reflect.Uint64:
			return "Long"
		case reflect.Float32, reflect.Float64, reflect.Complex64,
			reflect.Complex128:
			return "double"
		case reflect.String:
			return "String"
		case reflect.Array, reflect.Slice:
			return g.javaTypeArrayList(t.Elem())
		case reflect.Map:
			return "java.util.Map<String," + g.javaTypeWrapPrimitive(t.Elem()) + ">"
		default:
			if name == "Time" {
				return "String"
			}
			if name == "BoolValue"{
				return "java.lang.Boolean"
			}
			if len(name) == 0 && t.NumField() == 0 {
				return "Object"
			}
			return name
		}
	}
}

func (g *schemaGenerator) javaInterfaces(t reflect.Type) []string {
	if _, ok := t.FieldByName("ObjectMeta"); t.Name() != "JobTemplateSpec" && t.Name() != "PodTemplateSpec" && ok {
		return []string{"io.fabric8.kubernetes.api.model.HasMetadata"}
	}

	_, hasItems := t.FieldByName("Items")
	_, hasListMeta := t.FieldByName("ListMeta")
	if hasItems && hasListMeta {
		return []string{"io.fabric8.kubernetes.api.model.KubernetesResource", "io.fabric8.kubernetes.api.model.KubernetesResourceList"}
	}
	return []string{"io.fabric8.kubernetes.api.model.KubernetesResource"}
}

func (g *schemaGenerator) generate(t reflect.Type) (*JSONSchema, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Only struct types can be converted.")
	}

	s := JSONSchema{
		ID:     "http://snowdrop.me/istio/v1/" + t.Name() + "#",
		Schema: "http://json-schema.org/schema#",
		JSONDescriptor: JSONDescriptor{
			Type: "object",
		},
	}
	s.JSONObjectDescriptor = g.generateObjectDescriptor(t)
	if len(g.types) > 0 {
		s.Definitions = make(map[string]JSONPropertyDescriptor)
		s.Resources = make(map[string]*JSONObjectDescriptor)

		for k, v := range g.types {
			name := g.qualifiedName(k)
			resource := g.resourceDetails(k)
			value := JSONPropertyDescriptor{
				JSONDescriptor: &JSONDescriptor{
					Type: "object",
				},
				JSONObjectDescriptor: v,
				JavaTypeDescriptor: &JavaTypeDescriptor{
					JavaType: g.javaType(k),
				},
				JavaInterfacesDescriptor: &JavaInterfacesDescriptor{
					JavaInterfaces: g.javaInterfaces(k),
				},
			}
			s.Definitions[name] = value
			s.Resources[resource] = v
		}
	}

	return &s, nil
}

func (g *schemaGenerator) getPropertyDescriptor(t reflect.Type, desc string) JSONPropertyDescriptor {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	tt, ok := g.typeMap[t]
	if ok {
		t = tt
	}
	switch t.Kind() {
	case reflect.Bool:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "boolean",
				Description: desc,
			},
		}
	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "integer",
				Description: desc,
			},
		}
	case reflect.Int64, reflect.Uint64:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "integer",
				Description: desc,
			},
			JavaTypeDescriptor: &JavaTypeDescriptor{
				JavaType: "Long",
			},
		}
	case reflect.Float32, reflect.Float64, reflect.Complex64,
		reflect.Complex128:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "number",
				Description: desc,
			},
		}
	case reflect.String:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "string",
				Description: desc,
			},
		}
	case reflect.Array:
	case reflect.Slice:
		if g.javaTypeArrayList(t.Elem()) == "String" {
			return JSONPropertyDescriptor{
				JSONDescriptor: &JSONDescriptor{
					Type:        "string",
					Description: desc,
				},
			}
		} else {
			return JSONPropertyDescriptor{
				JSONDescriptor: &JSONDescriptor{
					Type:        "array",
					Description: desc,
				},
				JSONArrayDescriptor: &JSONArrayDescriptor{
					Items: g.getPropertyDescriptor(t.Elem(), desc),
				},
			}
		}
	case reflect.Map:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type:        "object",
				Description: desc,
			},
			JSONMapDescriptor: &JSONMapDescriptor{
				MapValueType: g.getPropertyDescriptor(t.Elem(), desc),
			},
			JavaTypeDescriptor: &JavaTypeDescriptor{
				JavaType: "java.util.Map<String," + g.javaTypeWrapPrimitive(t.Elem()) + ">",
			},
		}
	case reflect.Struct:
		definedType, ok := g.types[t]
		if !ok {
			g.types[t] = &JSONObjectDescriptor{}
			definedType = g.generateObjectDescriptor(t)
			g.types[t] = definedType
		}
		return JSONPropertyDescriptor{
			JSONReferenceDescriptor: &JSONReferenceDescriptor{
				Reference: g.generateReference(t),
			},
			JavaTypeDescriptor: &JavaTypeDescriptor{
				JavaType: g.javaType(t),
			},
		}
	}

	return JSONPropertyDescriptor{}
}

func (g *schemaGenerator) getStructProperties(t reflect.Type) map[string]JSONPropertyDescriptor {
	props := map[string]JSONPropertyDescriptor{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if len(field.PkgPath) > 0 { // Skip private fields
			continue
		}
		name := getFieldName(field)
		// Skip unserialized fields
		if name == "-" {
			continue
		}
		// Skip dockerImageMetadata field
		path := pkgPath(t)
		if path == "github.com/openshift/origin/pkg/image/api/v1" && t.Name() == "Image" && name == "dockerImageMetadata" {
			continue
		}

		desc := getFieldDescription(field)
		prop := g.getPropertyDescriptor(field.Type, desc)
		if field.Anonymous && field.Type.Kind() == reflect.Struct && len(name) == 0 {
			var newProps map[string]JSONPropertyDescriptor
			if prop.JSONReferenceDescriptor != nil {
				pType := field.Type
				if pType.Kind() == reflect.Ptr {
					pType = pType.Elem()
				}
				newProps = g.types[pType].Properties
			} else {
				newProps = prop.Properties
			}
			for k, v := range newProps {
				switch k {
				case "kind":
					v = JSONPropertyDescriptor{
						JSONDescriptor: &JSONDescriptor{
							Type:     "string",
							Default:  t.Name(),
							Required: true,
						},
					}
				case "apiVersion":
					apiVersion := filepath.Base(path)
					apiGroup := filepath.Base(strings.TrimSuffix(path, apiVersion))
					if apiGroup != "api" {
						groupPostfix := ""
						if strings.HasPrefix(path, "github.com/openshift/origin/pkg/") {
							groupPostfix = ".openshift.io"
						}
						apiVersion = apiGroup + groupPostfix + "/" + apiVersion
					}
					v = JSONPropertyDescriptor{
						JSONDescriptor: &JSONDescriptor{
							Type: "string",
						},
					}
					if apiVersion != "unversioned" {
						v.Required = true
						v.Default = apiVersion
					}
				default:
					g.addConstraints(t.Name(), k, &v)
				}
				props[k] = v
			}
		} else {
			g.addConstraints(t.Name(), name, &prop)
			props[name] = prop
		}
	}
	return props
}

func (g *schemaGenerator) generateObjectDescriptor(t reflect.Type) *JSONObjectDescriptor {
	desc := JSONObjectDescriptor{AdditionalProperties: true}
	desc.Properties = g.getStructProperties(t)
	return &desc
}

func (g *schemaGenerator) addConstraints(objectName string, propName string, prop *JSONPropertyDescriptor) {
	switch objectName {
	case "ObjectMeta":
		switch propName {
		case "namespace":
			prop.Pattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
			prop.MaxLength = 253
		}
	case "EnvVar":
		switch propName {
		case "name":
			prop.Pattern = `^[A-Za-z_][A-Za-z0-9_]*$`
		}
	case "Container", "Volume", "ContainePort", "ContainerStatus", "ServicePort", "EndpointPort":
		switch propName {
		case "name":
			prop.Pattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
			prop.MaxLength = 63
		}
	}
}

func pkgPath(t reflect.Type) string {
	path := t.PkgPath()
	return strings.TrimPrefix(path, "github.com/fabric8io/kubernetes-model/vendor/")
}