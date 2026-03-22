# CreateTypeInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**email_body** | **str** | Email body template | [optional] 
**email_subject** | **str** | Email subject template | [optional] 
**group_id** | **str** | ID of the group this type belongs to | 
**inbox_body** | **str** | Inbox notification body template | [optional] 
**inbox_title** | **str** | Inbox notification title template | [optional] 
**name** | **str** | Human-readable name | 
**slug** | **str** | URL-friendly identifier | 
**sms_body** | **str** | SMS body template | [optional] 

## Example

```python
from hermes_server_sdk.models.create_type_input_body import CreateTypeInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateTypeInputBody from a JSON string
create_type_input_body_instance = CreateTypeInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateTypeInputBody.to_json())

# convert the object into a dict
create_type_input_body_dict = create_type_input_body_instance.to_dict()
# create an instance of CreateTypeInputBody from a dict
create_type_input_body_from_dict = CreateTypeInputBody.from_dict(create_type_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


