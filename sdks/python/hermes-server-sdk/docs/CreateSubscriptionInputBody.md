# CreateSubscriptionInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**name** | **str** | Human-readable name | 
**slug** | **str** | URL-friendly identifier | 
**sort_order** | **int** | Display order within category | [optional] 

## Example

```python
from hermes_server_sdk.models.create_subscription_input_body import CreateSubscriptionInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateSubscriptionInputBody from a JSON string
create_subscription_input_body_instance = CreateSubscriptionInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateSubscriptionInputBody.to_json())

# convert the object into a dict
create_subscription_input_body_dict = create_subscription_input_body_instance.to_dict()
# create an instance of CreateSubscriptionInputBody from a dict
create_subscription_input_body_from_dict = CreateSubscriptionInputBody.from_dict(create_subscription_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


