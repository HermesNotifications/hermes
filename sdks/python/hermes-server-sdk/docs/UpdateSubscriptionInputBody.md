# UpdateSubscriptionInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**name** | **str** | Human-readable name | 
**sort_order** | **int** | Display order within category | 

## Example

```python
from hermes_server_sdk.models.update_subscription_input_body import UpdateSubscriptionInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of UpdateSubscriptionInputBody from a JSON string
update_subscription_input_body_instance = UpdateSubscriptionInputBody.from_json(json)
# print the JSON string representation of the object
print(UpdateSubscriptionInputBody.to_json())

# convert the object into a dict
update_subscription_input_body_dict = update_subscription_input_body_instance.to_dict()
# create an instance of UpdateSubscriptionInputBody from a dict
update_subscription_input_body_from_dict = UpdateSubscriptionInputBody.from_dict(update_subscription_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


