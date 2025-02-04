/**
 * Copyright 2021 Comcast Cable Communications Management, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package basculechecks

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorWithReason(t *testing.T) {
	assert := assert.New(t)
	testErr := errors.New("test err")
	e := errWithReason{
		err:    testErr,
		reason: "who knows",
	}
	var r Reasoner = e
	assert.Equal("who knows", r.Reason())

	var ee error = e
	assert.Equal("test err", ee.Error())

	assert.Equal(testErr, e.Unwrap())
}
