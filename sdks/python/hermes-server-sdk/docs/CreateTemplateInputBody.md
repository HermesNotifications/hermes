# CreateTemplateInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**default_channels** | **List[str]** | Default channels (used when no subscription) | [optional] 
**email_body** | **str** | Email body template (HTML) | [optional] 
**email_subject** | **str** | Email subject template | [optional] 
**inbox_body** | **str** | Inbox body template | [optional] 
**inbox_title** | **str** | Inbox title template | [optional] 
**name** | **str** | Human-readable name | 
**slug** | **str** | URL-friendly identifier | 
**sms_body** | **str** | SMS body template | [optional] 
**subscription_id** | **str** | Subscription ID (null for standalone) | [optional] 

## Example

```python
from hermes_server_sdk.models.create_template_input_body import CreateTemplateInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateTemplateInputBody from a JSON string
create_template_input_body_instance = CreateTemplateInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateTemplateInputBody.to_json())

# convert the object into a dict
create_template_input_body_dict = create_template_input_body_instance.to_dict()
# create an instance of CreateTemplateInputBody from a dict
create_template_input_body_from_dict = CreateTemplateInputBody.from_dict(create_template_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


