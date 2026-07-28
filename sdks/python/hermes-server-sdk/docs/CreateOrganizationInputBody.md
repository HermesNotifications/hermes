# CreateOrganizationInputBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_schema** | **str** | A URL to the JSON Schema for this object. | [optional] [readonly] 
**name** | **str** | Organization name | 

## Example

```python
from hermes_server_sdk.models.create_organization_input_body import CreateOrganizationInputBody

# TODO update the JSON string below
json = "{}"
# create an instance of CreateOrganizationInputBody from a JSON string
create_organization_input_body_instance = CreateOrganizationInputBody.from_json(json)
# print the JSON string representation of the object
print(CreateOrganizationInputBody.to_json())

# convert the object into a dict
create_organization_input_body_dict = create_organization_input_body_instance.to_dict()
# create an instance of CreateOrganizationInputBody from a dict
create_organization_input_body_from_dict = CreateOrganizationInputBody.from_dict(create_organization_input_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


