// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
)

var _ zapcore.ObjectMarshaler = ObjectReferceLogWrapper{}

// ObjectReferceLogWrapper implements zapcore.ObjectMarshaler which can be used to add a brief description of an
// ObjectReference as a log field.
// Workaround for https://github.com/kubernetes-sigs/controller-runtime/issues/1290
type ObjectReferceLogWrapper struct {
	corev1.ObjectReference
}

// MarshalLogObject marshals the object reference as a log field.
func (o ObjectReferceLogWrapper) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("apiVersion", o.APIVersion)
	enc.AddString("kind", o.Kind)

	ns := o.Namespace
	if ns != "" {
		enc.AddString("namespace", ns)
	}
	enc.AddString("name", o.Name)

	return nil
}
