# UpdateTypeInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**email_body** | **str** | Email body template | [optional] 
**email_subject** | **str** | Email subject template | [optional] 
**inbox_body** | **str** | Inbox notification body template | [optional] 
**inbox_title** | **str** | Inbox notification title template | [optional] 
**name** | **str** | Human-readable name | 
**sms_body** | **str** | SMS body template | [optional] 

## Example

```python
from hermes_server_sdk.models.update_type_input_body import UpdateTypeInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of UpdateTypeInputBody from a JSON string
update_type_input_body_instance = UpdateTypeInputBody.from_json(json)
# print the JSON string representation of the object
print(UpdateTypeInputBody.to_json())

# convert the object into a dict
update_type_input_body_dict = update_type_input_body_instance.to_dict()
# create an instance of UpdateTypeInputBody from a dict
update_type_input_body_from_dict = UpdateTypeInputBody.from_dict(update_type_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


