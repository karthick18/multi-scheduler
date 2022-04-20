/*
Copyright 2022 Ciena Corporation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"fmt"
	"hash/fnv"

	"k8s.io/apimachinery/pkg/util/rand"
)

func getName(namespace, podset string) string {
	hash := fnv.New32a()

	hash.Write([]byte(namespace))
	hash.Write([]byte(podset))

	name := podset + "-" + rand.SafeEncodeString(fmt.Sprint(hash.Sum32()))

	return name
}
