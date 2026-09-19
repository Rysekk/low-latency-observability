resource "oci_core_vcn" "vcn" {
  cidr_blocks    = ["10.0.0.0/16"]
  dns_label      = "vcn"
  compartment_id = oci_identity_compartment.tf-compartment.id
  display_name   = "vcn"
}

resource "oci_core_internet_gateway" "igw" {
  compartment_id = oci_identity_compartment.tf-compartment.id
  vcn_id         = oci_core_vcn.vcn.id
  display_name   = "internet_gateway"
  enabled        = true
}

resource "oci_core_route_table" "rt" {
  compartment_id = oci_identity_compartment.tf-compartment.id
  vcn_id         = oci_core_vcn.vcn.id
  display_name   = "route_table"

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.igw.id
  }
}

resource "oci_core_security_list" "security_list" {
  compartment_id = oci_identity_compartment.tf-compartment.id
  vcn_id         = oci_core_vcn.vcn.id
  display_name   = "security_list"
    
  ingress_security_rules {
    protocol  = "6" // tcp
    source    = var.my_ip
    stateless = false

    tcp_options {
      min=22
      max=22
    }
  }
  egress_security_rules {
		destination = "0.0.0.0/0"
		protocol = "all"
    destination_type = "CIDR_BLOCK"
    stateless = false
  }
}

resource "oci_core_subnet" "subnet" {
    compartment_id = oci_identity_compartment.tf-compartment.id
    vcn_id = oci_core_vcn.vcn.id
    route_table_id = oci_core_route_table.rt.id
    cidr_block = "10.0.1.0/24"
    security_list_ids = [ oci_core_security_list.security_list.id ]
    prohibit_public_ip_on_vnic = false
}