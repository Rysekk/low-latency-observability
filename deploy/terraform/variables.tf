variable "tenancy_ocid" {
    description = "tenancy of the oci account"
    type    = string
}

variable "user_ocid" {
    description = "User of the OCI account"
    type    = string
}

variable "fingerprint" {
    description = "Fingerprint to user the API"
    type    = string
}

variable "region" {
    description = "Region of the OCI account"
    type    = string
}

variable "oci_private_key_path" {
    description = "Local folder of the OCI API private key"
    type    = string
}

variable "my_ip" {
    description = "My public ipv4"
    type    = string
}