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

package bascule

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidators(t *testing.T) {
	emptyAttributes := NewAttributes(map[string]interface{}{})
	testErr := errors.New("test err")
	var (
		failFunc ValidatorFunc = func(_ context.Context, _ Token) error {
			return testErr
		}
		successFunc ValidatorFunc = func(_ context.Context, _ Token) error {
			return nil
		}
	)
	assert := assert.New(t)
	validatorF := Validators([]Validator{successFunc, failFunc})
	validatorS := Validators([]Validator{successFunc, successFunc, successFunc})
	err := validatorS.Check(context.Background(), NewToken("type", "principal", emptyAttributes))
	assert.NoError(err)
	errs := validatorF.Check(context.Background(), NewToken("", "", emptyAttributes))
	assert.NotNil(errs)
	assert.True(errors.As(errs, &Errors{}))
}
